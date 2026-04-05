package cicd

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/storage"
)

// WebhookServer принимает входящие webhook от GitHub/GitLab/Jenkins
type WebhookServer struct {
	pipelineRepo storage.PipelineRepo
	notifier     *Notifier
	secret       string
	log          *zap.Logger
}

// NewWebhookServer создаёт HTTP-сервер для приёма webhook
func NewWebhookServer(pipelineRepo storage.PipelineRepo, notifier *Notifier, secret string, log *zap.Logger) *WebhookServer {
	return &WebhookServer{
		pipelineRepo: pipelineRepo,
		notifier:     notifier,
		secret:       secret,
		log:          log,
	}
}

// Start запускает HTTP-сервер на указанном порту.
// блокирует до отмены контекста, затем выполняет graceful shutdown.
func (s *WebhookServer) Start(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/github", s.handleGitHub)
	mux.HandleFunc("/webhook/gitlab", s.handleGitLab)
	mux.HandleFunc("/webhook/jenkins", s.handleJenkins)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		s.log.Info("webhook-сервер запущен", zap.Int("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("ошибка webhook-сервера", zap.Error(err))
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		s.log.Error("ошибка остановки webhook-сервера", zap.Error(err))
	}
	s.log.Info("webhook-сервер остановлен")
}

// handleGitHub обрабатывает workflow_run webhook от GitHub Actions
func (s *WebhookServer) handleGitHub(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readAndVerifyGitHub(w, r)
	if !ok {
		return
	}

	// обрабатываем только события workflow_run
	if r.Header.Get("X-GitHub-Event") != "workflow_run" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload githubWorkflowPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		s.log.Warn("ошибка парсинга GitHub payload", zap.Error(err))
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	e := &storage.PipelineEvent{
		PipelineID: fmt.Sprintf("%d", payload.WorkflowRun.ID),
		Source:     "github",
		Repo:       payload.Repository.FullName,
		Branch:     payload.WorkflowRun.HeadBranch,
		Status:     githubStatus(payload.WorkflowRun.Conclusion),
		Author:     payload.WorkflowRun.Actor.Login,
		Payload:    body,
		CreatedAt:  time.Now(),
	}

	s.saveAndNotify(r.Context(), e)
	w.WriteHeader(http.StatusOK)
}

// handleGitLab обрабатывает Pipeline Hook webhook от GitLab CI
func (s *WebhookServer) handleGitLab(w http.ResponseWriter, r *http.Request) {
	// GitLab использует заголовок X-Gitlab-Token вместо HMAC
	if s.secret != "" && r.Header.Get("X-Gitlab-Token") != s.secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if r.Header.Get("X-Gitlab-Event") != "Pipeline Hook" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload gitlabPipelinePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		s.log.Warn("ошибка парсинга GitLab payload", zap.Error(err))
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	e := &storage.PipelineEvent{
		PipelineID: fmt.Sprintf("%d", payload.ObjectAttributes.ID),
		Source:     "gitlab",
		Repo:       payload.Project.PathWithNamespace,
		Branch:     payload.ObjectAttributes.Ref,
		Status:     gitlabStatus(payload.ObjectAttributes.Status),
		Author:     payload.User.Username,
		Payload:    body,
		CreatedAt:  time.Now(),
	}

	s.saveAndNotify(r.Context(), e)
	w.WriteHeader(http.StatusOK)
}

// handleJenkins обрабатывает webhook от Jenkins
func (s *WebhookServer) handleJenkins(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var payload jenkinsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		s.log.Warn("ошибка парсинга Jenkins payload", zap.Error(err))
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	e := &storage.PipelineEvent{
		PipelineID: fmt.Sprintf("%d", payload.Build.Number),
		Source:     "jenkins",
		Repo:       payload.Name,
		Branch:     payload.Build.Branch,
		Status:     jenkinsStatus(payload.Build.Phase, payload.Build.Status),
		Author:     payload.Build.UserID,
		Payload:    body,
		CreatedAt:  time.Now(),
	}

	s.saveAndNotify(r.Context(), e)
	w.WriteHeader(http.StatusOK)
}

// saveAndNotify сохраняет событие в БД и отправляет Telegram-уведомление
func (s *WebhookServer) saveAndNotify(ctx context.Context, e *storage.PipelineEvent) {
	if err := s.pipelineRepo.Create(ctx, e); err != nil {
		s.log.Error("ошибка сохранения pipeline_event", zap.Error(err))
		return
	}
	s.notifier.Notify(ctx, e)
}

// readAndVerifyGitHub читает тело и проверяет HMAC-подпись X-Hub-Signature-256
func (s *WebhookServer) readAndVerifyGitHub(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return nil, false
	}

	if s.secret != "" {
		if !verifyGitHubSig(body, s.secret, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return nil, false
		}
	}

	return body, true
}

// verifyGitHubSig проверяет подпись HMAC-SHA256 из заголовка X-Hub-Signature-256
func verifyGitHubSig(body []byte, secret, sigHeader string) bool {
	if !strings.HasPrefix(sigHeader, "sha256=") {
		return false
	}
	received, err := hex.DecodeString(strings.TrimPrefix(sigHeader, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(received, mac.Sum(nil))
}

// ----- структуры payload для разбора входящих webhook -----

type githubWorkflowPayload struct {
	WorkflowRun struct {
		ID         int64  `json:"id"`
		HeadBranch string `json:"head_branch"`
		Conclusion string `json:"conclusion"`
		Actor      struct {
			Login string `json:"login"`
		} `json:"actor"`
	} `json:"workflow_run"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type gitlabPipelinePayload struct {
	ObjectAttributes struct {
		ID     int    `json:"id"`
		Ref    string `json:"ref"`
		Status string `json:"status"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}

type jenkinsPayload struct {
	Name  string `json:"name"`
	Build struct {
		Number int    `json:"number"`
		Phase  string `json:"phase"`
		Status string `json:"status"`
		Branch string `json:"scm_branch"`
		UserID string `json:"user_id"`
	} `json:"build"`
}

// ----- маппинг внешних статусов в внутренние константы -----

func githubStatus(conclusion string) string {
	switch conclusion {
	case "success":
		return storage.PipelineStatusSuccess
	case "failure", "cancelled", "timed_out":
		return storage.PipelineStatusFailed
	default:
		return storage.PipelineStatusRunning
	}
}

func gitlabStatus(status string) string {
	switch status {
	case "success":
		return storage.PipelineStatusSuccess
	case "failed":
		return storage.PipelineStatusFailed
	case "running":
		return storage.PipelineStatusRunning
	default:
		return storage.PipelineStatusPending
	}
}

func jenkinsStatus(phase, status string) string {
	switch phase {
	case "COMPLETED":
		if status == "SUCCESS" {
			return storage.PipelineStatusSuccess
		}
		return storage.PipelineStatusFailed
	case "STARTED":
		return storage.PipelineStatusRunning
	default:
		return storage.PipelineStatusPending
	}
}
