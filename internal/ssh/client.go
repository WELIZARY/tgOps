package ssh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	gossh "golang.org/x/crypto/ssh"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Client - SSH-клиент с пулом соединений на каждый хост
type Client struct {
	cfg   *config.SSHConfig
	pools map[string]chan *gossh.Client // "host:port" -> канал свободных соединений
	mu    sync.Mutex
	log   *zap.Logger
}

// New создаёт SSH-клиент
func New(cfg *config.SSHConfig, log *zap.Logger) *Client {
	return &Client{
		cfg:   cfg,
		pools: make(map[string]chan *gossh.Client),
		log:   log,
	}
}

// ServerSpec - параметры подключения к конкретному серверу
type ServerSpec struct {
	Host    string
	Port    int
	User    string
	// KeyName - имя файла ключа в KeysDir; если пусто - используется DefaultKeyPath
	KeyName string
}

// SpecFromServer конвертирует запись сервера из БД или конфига в ServerSpec
func SpecFromServer(s *storage.Server) ServerSpec {
	port := s.Port
	if port == 0 {
		port = 22
	}
	return ServerSpec{
		Host:    s.Host,
		Port:    port,
		User:    s.SSHUser,
		KeyName: s.KeyName,
	}
}

// Run выполняет команду на сервере и возвращает вывод (stdout + stderr).
// Берёт соединение из пула, после выполнения возвращает его обратно.
func (c *Client) Run(ctx context.Context, spec ServerSpec, command string) (string, error) {
	conn, err := c.getConn(spec)
	if err != nil {
		return "", fmt.Errorf("подключение к %s: %w", spec.Host, err)
	}

	sess, err := conn.NewSession()
	if err != nil {
		// Соединение протухло - не возвращаем в пул
		_ = conn.Close()
		return "", fmt.Errorf("сессия на %s: %w", spec.Host, err)
	}
	defer sess.Close() //nolint:errcheck

	// Таймаут команды через контекст
	cmdTimeout, err := time.ParseDuration(c.cfg.CommandTimeout)
	if err != nil {
		cmdTimeout = 30 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	// Закрываем сессию при отмене контекста
	done := make(chan struct{})
	go func() {
		select {
		case <-cmdCtx.Done():
			_ = sess.Close()
		case <-done:
		}
	}()

	out, runErr := sess.CombinedOutput(command)
	close(done)

	// Соединение живое - возвращаем в пул (сессия закрылась, а не соединение)
	c.putConn(spec, conn)

	if cmdCtx.Err() != nil {
		return "", fmt.Errorf("команда на %s: таймаут или отмена", spec.Host)
	}
	return strings.TrimSpace(string(out)), runErr
}

// getConn берёт соединение из пула или создаёт новое
func (c *Client) getConn(spec ServerSpec) (*gossh.Client, error) {
	pool := c.getPool(spec)

	// Пробуем взять готовое соединение
	select {
	case conn := <-pool:
		// Проверяем что соединение ещё живое
		if _, _, err := conn.SendRequest("keepalive@openssh.com", true, nil); err == nil {
			return conn, nil
		}
		_ = conn.Close()
	default:
	}

	return c.dial(spec)
}

// putConn возвращает соединение в пул; если пул заполнен - закрывает соединение
func (c *Client) putConn(spec ServerSpec, conn *gossh.Client) {
	pool := c.getPool(spec)
	select {
	case pool <- conn:
	default:
		_ = conn.Close()
	}
}

// getPool возвращает (или создаёт) канал-пул для данного хоста
func (c *Client) getPool(spec ServerSpec) chan *gossh.Client {
	key := fmt.Sprintf("%s:%d", spec.Host, spec.Port)

	c.mu.Lock()
	defer c.mu.Unlock()

	if pool, ok := c.pools[key]; ok {
		return pool
	}

	maxConn := c.cfg.MaxConnectionsPerHost
	if maxConn <= 0 {
		maxConn = 3
	}
	pool := make(chan *gossh.Client, maxConn)
	c.pools[key] = pool
	return pool
}

// dial устанавливает новое SSH-соединение
func (c *Client) dial(spec ServerSpec) (*gossh.Client, error) {
	keyPath := c.resolveKeyPath(spec.KeyName)
	signer, err := loadPrivateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("загрузка ключа %q: %w", keyPath, err)
	}

	connectTimeout, err := time.ParseDuration(c.cfg.ConnectTimeout)
	if err != nil {
		connectTimeout = 10 * time.Second
	}

	sshCfg := &gossh.ClientConfig{
		User:            spec.User,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // для дипломного проекта приемлемо
		Timeout:         connectTimeout,
	}

	addr := fmt.Sprintf("%s:%d", spec.Host, spec.Port)
	conn, err := gossh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	c.log.Debug("SSH соединение установлено",
		zap.String("host", spec.Host),
		zap.Int("port", spec.Port),
	)
	return conn, nil
}

// resolveKeyPath возвращает полный путь к приватному ключу
func (c *Client) resolveKeyPath(keyName string) string {
	if keyName == "" {
		return c.cfg.DefaultKeyPath
	}
	return filepath.Join(c.cfg.KeysDir, keyName)
}

// loadPrivateKey читает и парсит приватный ключ из файла
func loadPrivateKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение файла: %w", err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("парсинг ключа: %w", err)
	}
	return signer, nil
}
