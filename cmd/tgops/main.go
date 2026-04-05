package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/audit"
	tgbot "github.com/WELIZARY/tgOps/internal/bot"
	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/modules/alerts"
	"github.com/WELIZARY/tgOps/internal/modules/cicd"
	"github.com/WELIZARY/tgOps/internal/modules/core"
	"github.com/WELIZARY/tgOps/internal/modules/docker"
	"github.com/WELIZARY/tgOps/internal/modules/logs"
	"github.com/WELIZARY/tgOps/internal/modules/network"
	"github.com/WELIZARY/tgOps/internal/modules/ssl"
	"github.com/WELIZARY/tgOps/internal/modules/system"
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
	"github.com/WELIZARY/tgOps/internal/storage"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "путь к файлу конфигурации")
	flag.Parse()

	// Контекст: завершается по SIGTERM или SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Загрузка конфига
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		panic("не удалось загрузить конфиг: " + err.Error())
	}

	// Инициализация логгера
	log, err := buildLogger(cfg.Logger)
	if err != nil {
		panic("не удалось создать логгер: " + err.Error())
	}
	defer log.Sync() //nolint:errcheck

	log.Info("tgOPS запускается",
		zap.String("config", *cfgPath),
		zap.String("log_level", cfg.Logger.Level),
	)

	// Подключение к PostgreSQL
	db, err := storage.Connect(ctx, cfg.Database, log)
	if err != nil {
		log.Fatal("не удалось подключиться к БД", zap.Error(err))
	}
	defer db.Close()

	// Применяем миграции
	if err := storage.RunMigrations(ctx, db, "migrations", log); err != nil {
		log.Fatal("ошибка применения миграций", zap.Error(err))
	}

	// Репозитории
	userRepo := storage.NewUserRepo(db)
	auditRepo := storage.NewAuditRepo(db)
	alertRepo := storage.NewAlertRepo(db)
	sslRepo := storage.NewSSLRepo(db)
	serverRepo := storage.NewServerRepo(db)
	pipelineRepo := storage.NewPipelineRepo(db)

	// Bootstrap первого администратора (если БД пустая)
	if err := bootstrapAdmin(ctx, cfg, userRepo, log); err != nil {
		log.Fatal("ошибка bootstrap администратора", zap.Error(err))
	}

	// Источник серверов: конфиг + БД
	serverSrc := internalssh.NewComboSource(&cfg.SSH, serverRepo)

	// SSH-клиент с пулом соединений
	sshClient := internalssh.New(&cfg.SSH, log)

	// Audit logger
	auditLogger := audit.New(auditRepo, log)

	// Router - центральный диспетчер команд
	router := tgbot.NewRouter(userRepo, auditLogger, log)

	// Создаём бота до модулей - alertMgr нужен bot.API()
	b, err := tgbot.New(cfg.Telegram.Token, router, log)
	if err != nil {
		log.Fatal("не удалось создать Telegram-бота", zap.Error(err))
	}

	// Alert manager - отправляет уведомления в чат
	alertMgr := alerts.NewManager(b.API(), cfg.Notify.ChatID, log)

	// CI/CD notifier - отправляет уведомления о деплоях
	cicdNotifier := cicd.NewNotifier(b.API(), cfg.CICD.NotifyChatID, pipelineRepo, log)

	// Webhook-сервер для приёма событий от GitHub/GitLab/Jenkins
	webhookSrv := cicd.NewWebhookServer(pipelineRepo, cicdNotifier, cfg.CICD.WebhookSecret, log)

	// Модули
	coreModule := core.New(func(role string) []modules.BotCommand {
		return router.CommandsForRole(role)
	})
	systemMod := system.New(sshClient, serverSrc, cfg, log)
	alertsMod := alerts.New(alertRepo, log)
	sslMod := ssl.New(&cfg.SSL, sslRepo, alertMgr, log)
	networkMod := network.New(log)
	logsMod := logs.New(sshClient, serverSrc, &cfg.Logs, log)
	dockerMod := docker.New(sshClient, serverSrc, &cfg.Docker, log)
	cicdMod := cicd.New(pipelineRepo, cicdNotifier, log)

	// Регистрируем модули
	router.Register(coreModule)
	router.Register(systemMod)
	router.Register(alertsMod)
	router.Register(sslMod)
	router.Register(networkMod)
	router.Register(logsMod)
	router.Register(dockerMod)
	router.Register(cicdMod)

	// Callback-обработчики (inline-кнопки)
	router.RegisterCallback("ack_", alertsMod.HandleAck)
	router.RegisterCallback("deploy_approve_", cicdMod.HandleApprove)
	router.RegisterCallback("deploy_reject_", cicdMod.HandleReject)

	// Фоновые горутины мониторинга
	collector := alerts.NewCollector(sshClient, serverSrc, cfg, alertRepo, alertMgr, log)
	go collector.Start(ctx)
	go sslMod.Checker().Start(ctx)
	go webhookSrv.Start(ctx, cfg.CICD.WebhookPort)

	log.Info("все модули зарегистрированы, запускаем бота")
	b.Start(ctx) // блокирует до сигнала завершения

	log.Info("tgOPS остановлен")
}

// bootstrapAdmin создаёт первого администратора из конфига, если таблица users пустая
func bootstrapAdmin(ctx context.Context, cfg *config.Config, repo storage.UserRepo, log *zap.Logger) error {
	if cfg.Telegram.InitialAdminID == 0 {
		return nil
	}

	count, err := repo.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	admin := &storage.User{
		TelegramID: cfg.Telegram.InitialAdminID,
		Username:   "admin",
		Role:       storage.RoleAdmin,
		IsActive:   true,
	}
	if err := repo.Create(ctx, admin); err != nil {
		return err
	}

	log.Info("bootstrap: создан первый администратор",
		zap.Int64("telegram_id", cfg.Telegram.InitialAdminID),
	)
	return nil
}

// buildLogger создаёт zap.Logger по настройкам из конфига
func buildLogger(cfg config.LoggerConfig) (*zap.Logger, error) {
	var zapCfg zap.Config

	if cfg.Level == "debug" {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}

	level, err := zap.ParseAtomicLevel(cfg.Level)
	if err != nil {
		level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	zapCfg.Level = level

	return zapCfg.Build()
}
