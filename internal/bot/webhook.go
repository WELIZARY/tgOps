package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// WebhookOptions - параметры webhook-сервера бота
type WebhookOptions struct {
	// Addr - на каком адресе слушать (например ":8080")
	Addr string
	// Secret - секрет в path /webhook/<secret> + проверка X-Telegram-Bot-Api-Secret-Token
	Secret string
	// AlertSecret - Bearer-токен для /internal/alert (Cloud Monitoring), пусто = роут выключен
	AlertSecret string
	// JenkinsSecret - X-Jenkins-Secret для /internal/jenkins, пусто = роут выключен
	JenkinsSecret string
	// NotifyChatID - куда слать алерты и события Jenkins
	NotifyChatID int64
}

// StartWebhook поднимает http-сервер с роутами:
//   - GET  /healthz                - health-check для LB
//   - POST /webhook/<secret>       - входящие Update от Telegram
//   - POST /internal/alert         - алерты от Cloud Monitoring (опционально)
//   - POST /internal/jenkins       - события от Jenkins-пайплайнов (опционально)
//
// Блокирует до отмены контекста.
func (b *Bot) StartWebhook(ctx context.Context, opts WebhookOptions) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	webhookPath := "/webhook/" + opts.Secret
	mux.HandleFunc(webhookPath, b.handleTelegramUpdate(ctx, opts.Secret))

	if opts.AlertSecret != "" {
		mux.HandleFunc("/internal/alert", b.handleCloudAlert(opts.AlertSecret, opts.NotifyChatID))
	}
	if opts.JenkinsSecret != "" {
		mux.HandleFunc("/internal/jenkins", b.handleJenkinsEvent(opts.JenkinsSecret, opts.NotifyChatID))
	}

	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	b.log.Info("webhook-сервер запускается",
		zap.String("addr", opts.Addr),
		zap.String("telegram_path", webhookPath),
		zap.Bool("alert_route", opts.AlertSecret != ""),
		zap.Bool("jenkins_route", opts.JenkinsSecret != ""),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		b.log.Info("webhook-сервер останавливается")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (b *Bot) handleTelegramUpdate(ctx context.Context, secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Telegram дублирует секрет в заголовке - проверяем оба
		if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != secret {
			b.log.Warn("webhook: неверный secret token", zap.String("from", r.RemoteAddr))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var update tgbotapi.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			b.log.Warn("webhook: невалидный JSON", zap.Error(err))
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// обрабатываем в фоне, отвечаем 200 сразу
		if update.Message != nil {
			go b.router.Dispatch(ctx, b.api, update.Message)
		}
		if update.CallbackQuery != nil {
			go b.router.DispatchCallback(ctx, b.api, update.CallbackQuery)
		}

		w.WriteHeader(http.StatusOK)
	}
}

// формат Cloud Monitoring webhook
type cloudMonitoringIncident struct {
	IncidentID string `json:"incident_id"`
	Summary    string `json:"summary"`
	State      string `json:"state"`
	PolicyName string `json:"policy_name"`
	URL        string `json:"url"`
}

type cloudMonitoringPayload struct {
	Incident cloudMonitoringIncident `json:"incident"`
	Version  string                  `json:"version"`
}

func (b *Bot) handleCloudAlert(secret string, chatID int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var p cloudMonitoringPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		text := fmt.Sprintf(
			"🚨 *Cloud Monitoring*\n*Policy:* %s\n*State:* %s\n*Summary:* %s",
			p.Incident.PolicyName, p.Incident.State, p.Incident.Summary,
		)
		if p.Incident.URL != "" {
			text += "\n[ссылка](" + p.Incident.URL + ")"
		}
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.api.Send(msg); err != nil {
			b.log.Error("не удалось отправить cloud-alert", zap.Error(err))
		}
		w.WriteHeader(http.StatusOK)
	}
}

type jenkinsPayload struct {
	Job    string `json:"job"`
	Status string `json:"status"`
	Build  string `json:"build"`
	URL    string `json:"url"`
	Target string `json:"target,omitempty"`
}

func (b *Bot) handleJenkinsEvent(secret string, chatID int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Jenkins-Secret") != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var p jenkinsPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		icon := "✅"
		if p.Status != "success" {
			icon = "❌"
		}
		text := fmt.Sprintf("%s Jenkins *%s* #%s -> *%s*", icon, p.Job, p.Build, p.Status)
		if p.Target != "" {
			text += "\nцель: " + p.Target
		}
		if p.URL != "" {
			text += "\n" + p.URL
		}
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.api.Send(msg); err != nil {
			b.log.Error("не удалось отправить jenkins-сообщение", zap.Error(err))
		}
		w.WriteHeader(http.StatusOK)
	}
}
