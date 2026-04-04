package ssl

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/modules/alerts"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// CheckResult - результат проверки одного домена
type CheckResult struct {
	Domain    string
	Issuer    string
	ExpiresAt time.Time
	DaysLeft  int
	Status    string // ok, warning, critical, expired
	Error     error
}

// Checker проверяет SSL-сертификаты и сохраняет результаты в БД
type Checker struct {
	cfg      *config.SSLConfig
	sslRepo  storage.SSLRepo
	alertMgr *alerts.Manager
	log      *zap.Logger
}

// NewChecker создаёт SSL-чекер
func NewChecker(cfg *config.SSLConfig, sslRepo storage.SSLRepo, alertMgr *alerts.Manager, log *zap.Logger) *Checker {
	return &Checker{
		cfg:      cfg,
		sslRepo:  sslRepo,
		alertMgr: alertMgr,
		log:      log,
	}
}

// Start запускает фоновую проверку сертификатов по тикеру.
// Завершается по ctx.
func (c *Checker) Start(ctx context.Context) {
	interval, err := time.ParseDuration(c.cfg.CheckInterval)
	if err != nil {
		interval = 24 * time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Первая проверка сразу при старте
	c.checkAll(ctx)

	c.log.Info("SSL-чекер запущен", zap.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			c.log.Info("SSL-чекер остановлен")
			return
		case <-ticker.C:
			c.checkAll(ctx)
		}
	}
}

// checkAll проверяет все домены из конфига
func (c *Checker) checkAll(ctx context.Context) {
	for _, domain := range c.cfg.Domains {
		result := c.Check(domain)
		c.save(ctx, result)
		c.maybeAlert(result)
	}
}

// Check проверяет SSL-сертификат одного домена через TLS-подключение
func (c *Checker) Check(domain string) *CheckResult {
	result := &CheckResult{Domain: domain}

	conn, err := tls.Dial("tcp", domain+":443", &tls.Config{
		InsecureSkipVerify: false, //nolint:gosec
	})
	if err != nil {
		result.Error = fmt.Errorf("TLS-подключение: %w", err)
		result.Status = "expired"
		return result
	}
	defer conn.Close() //nolint:errcheck

	cert := conn.ConnectionState().PeerCertificates[0]
	result.ExpiresAt = cert.NotAfter
	result.DaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)

	// Issuer - короткое имя организации или CN
	if len(cert.Issuer.Organization) > 0 {
		result.Issuer = cert.Issuer.Organization[0]
	} else {
		result.Issuer = cert.Issuer.CommonName
	}

	switch {
	case result.DaysLeft <= 0:
		result.Status = "expired"
	case result.DaysLeft <= 7:
		result.Status = "critical"
	case result.DaysLeft <= 30:
		result.Status = "warning"
	default:
		result.Status = "ok"
	}

	return result
}

// CheckAndSave проверяет домен и сохраняет результат в БД
func (c *Checker) CheckAndSave(ctx context.Context, domain string) *CheckResult {
	result := c.Check(domain)
	c.save(ctx, result)
	return result
}

// save сохраняет результат проверки в БД
func (c *Checker) save(ctx context.Context, r *CheckResult) {
	if r.Error != nil {
		c.log.Warn("ошибка проверки SSL", zap.String("domain", r.Domain), zap.Error(r.Error))
		return
	}
	check := &storage.SSLCheck{
		Domain:    r.Domain,
		Issuer:    r.Issuer,
		ExpiresAt: r.ExpiresAt,
		DaysLeft:  r.DaysLeft,
		Status:    r.Status,
	}
	if err := c.sslRepo.Upsert(ctx, check); err != nil {
		c.log.Error("ошибка сохранения SSL-проверки", zap.String("domain", r.Domain), zap.Error(err))
	}
}

// maybeAlert отправляет уведомление если days_left входит в warn_days из конфига
func (c *Checker) maybeAlert(r *CheckResult) {
	if r.Error != nil || r.Status == "ok" {
		return
	}
	for _, warnDay := range c.cfg.WarnDays {
		// Допуск ±1 день чтобы не пропустить из-за времени запуска
		if r.DaysLeft <= warnDay && r.DaysLeft > warnDay-2 {
			severity := storage.SeverityWarning
			if r.Status == "critical" || r.Status == "expired" {
				severity = storage.SeverityCritical
			}
			c.alertMgr.SendAlert(&storage.Alert{
				ServerName: r.Domain,
				AlertType:  storage.AlertTypeSSL,
				Severity:   severity,
				Message:    fmt.Sprintf("SSL-сертификат %s истекает через %d дн. (%s)", r.Domain, r.DaysLeft, r.ExpiresAt.Format("02.01.2006")),
			})
			return
		}
	}
}
