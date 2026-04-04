package alerts

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/modules/system"
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Collector - фоновый сборщик метрик и HTTP-чеков
type Collector struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	alertRepo storage.AlertRepo
	alertMgr  *Manager
	cfg       *config.Config
	log       *zap.Logger
}

// NewCollector создаёт сборщик метрик
func NewCollector(
	sshClient *internalssh.Client,
	src internalssh.ServerSource,
	cfg *config.Config,
	alertRepo storage.AlertRepo,
	alertMgr *Manager,
	log *zap.Logger,
) *Collector {
	return &Collector{
		sshClient: sshClient,
		src:       src,
		alertRepo: alertRepo,
		alertMgr:  alertMgr,
		cfg:       cfg,
		log:       log,
	}
}

// Start запускает две горутины: опрос серверов и HTTP-чеки.
// Завершается по ctx.
func (c *Collector) Start(ctx context.Context) {
	interval, err := time.ParseDuration(c.cfg.Monitoring.Interval)
	if err != nil {
		interval = 60 * time.Second
	}

	httpInterval, err := time.ParseDuration(c.cfg.HealthChecks.Interval)
	if err != nil {
		httpInterval = 60 * time.Second
	}

	serverTicker := time.NewTicker(interval)
	httpTicker := time.NewTicker(httpInterval)
	defer serverTicker.Stop()
	defer httpTicker.Stop()

	c.log.Info("сборщик метрик запущен",
		zap.Duration("interval", interval),
		zap.Duration("http_interval", httpInterval),
	)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("сборщик метрик остановлен")
			return
		case <-serverTicker.C:
			c.checkServers(ctx)
		case <-httpTicker.C:
			c.checkHTTP(ctx)
		}
	}
}

// checkServers опрашивает все серверы параллельно
func (c *Collector) checkServers(ctx context.Context) {
	servers, err := c.src.GetServers(ctx)
	if err != nil {
		c.log.Error("ошибка получения списка серверов", zap.Error(err))
		return
	}
	for _, srv := range servers {
		go c.checkServer(ctx, srv)
	}
}

// checkServer собирает метрики одного сервера и создаёт алерты при необходимости
func (c *Collector) checkServer(ctx context.Context, srv *storage.Server) {
	t := c.cfg.Monitoring.Thresholds
	spec := internalssh.SpecFromServer(srv)
	metrics := system.Collect(ctx, c.sshClient, spec)

	// Сервер недоступен
	if metrics.Error != nil {
		c.maybeAlert(ctx, srv.Name, storage.AlertTypeServiceDown, storage.SeverityCritical,
			fmt.Sprintf("Сервер %s недоступен: %v", srv.Name, metrics.Error))
		return
	}

	// CPU
	if metrics.CPU >= t.CPUCritical {
		c.maybeAlert(ctx, srv.Name, storage.AlertTypeCPU, storage.SeverityCritical,
			fmt.Sprintf("CPU %.1f%% (порог: %.0f%%)", metrics.CPU, t.CPUCritical))
	} else if metrics.CPU >= t.CPUWarning {
		c.maybeAlert(ctx, srv.Name, storage.AlertTypeCPU, storage.SeverityWarning,
			fmt.Sprintf("CPU %.1f%% (порог: %.0f%%)", metrics.CPU, t.CPUWarning))
	}

	// RAM
	if metrics.RAMTotal > 0 {
		ramPct := float64(metrics.RAMUsed) / float64(metrics.RAMTotal) * 100
		if ramPct >= t.RAMCritical {
			c.maybeAlert(ctx, srv.Name, storage.AlertTypeRAM, storage.SeverityCritical,
				fmt.Sprintf("RAM %.1f%% (порог: %.0f%%)", ramPct, t.RAMCritical))
		} else if ramPct >= t.RAMWarning {
			c.maybeAlert(ctx, srv.Name, storage.AlertTypeRAM, storage.SeverityWarning,
				fmt.Sprintf("RAM %.1f%% (порог: %.0f%%)", ramPct, t.RAMWarning))
		}
	}

	// Disk
	if metrics.DiskTotal > 0 {
		diskPct := float64(metrics.DiskUsed) / float64(metrics.DiskTotal) * 100
		if diskPct >= t.DiskCritical {
			c.maybeAlert(ctx, srv.Name, storage.AlertTypeDisk, storage.SeverityCritical,
				fmt.Sprintf("Диск %.1f%% (порог: %.0f%%)", diskPct, t.DiskCritical))
		} else if diskPct >= t.DiskWarning {
			c.maybeAlert(ctx, srv.Name, storage.AlertTypeDisk, storage.SeverityWarning,
				fmt.Sprintf("Диск %.1f%% (порог: %.0f%%)", diskPct, t.DiskWarning))
		}
	}
}

// checkHTTP проверяет HTTP-эндпоинты из конфига
func (c *Collector) checkHTTP(ctx context.Context) {
	if len(c.cfg.HealthChecks.Endpoints) == 0 {
		return
	}

	timeout, err := time.ParseDuration(c.cfg.HealthChecks.Timeout)
	if err != nil {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	for _, ep := range c.cfg.HealthChecks.Endpoints {
		go c.checkEndpoint(ctx, client, ep)
	}
}

// checkEndpoint делает GET-запрос и алертит при неожиданном статусе
func (c *Collector) checkEndpoint(ctx context.Context, client *http.Client, ep config.EndpointConfig) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.URL, nil)
	if err != nil {
		c.log.Error("ошибка создания запроса", zap.String("endpoint", ep.Name), zap.Error(err))
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		c.maybeAlert(ctx, ep.Name, storage.AlertTypeHTTP, storage.SeverityCritical,
			fmt.Sprintf("Эндпоинт %s недоступен: %v", ep.Name, err))
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	expected := ep.ExpectedStatus
	if expected == 0 {
		expected = http.StatusOK
	}
	if resp.StatusCode != expected {
		c.maybeAlert(ctx, ep.Name, storage.AlertTypeHTTP, storage.SeverityCritical,
			fmt.Sprintf("Эндпоинт %s вернул %d (ожидался %d)", ep.Name, resp.StatusCode, expected))
	}
}

// maybeAlert создаёт алерт и отправляет уведомление,
// если нет уже активного алерта того же типа для того же сервера
func (c *Collector) maybeAlert(ctx context.Context, serverName, alertType, severity, message string) {
	active, err := c.alertRepo.HasActive(ctx, serverName, alertType)
	if err != nil {
		c.log.Error("ошибка проверки активных алертов", zap.Error(err))
		return
	}
	if active {
		return // уже есть активный алерт - не спамим
	}

	alert := &storage.Alert{
		ServerName: serverName,
		AlertType:  alertType,
		Severity:   severity,
		Message:    message,
	}
	if err := c.alertRepo.Create(ctx, alert); err != nil {
		c.log.Error("ошибка создания алерта", zap.Error(err))
		return
	}

	c.log.Info("создан алерт",
		zap.String("server", serverName),
		zap.String("type", alertType),
		zap.String("severity", severity),
	)
	c.alertMgr.SendAlert(alert)
}
