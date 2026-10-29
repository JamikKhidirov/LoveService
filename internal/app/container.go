package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/config"
	"github.com/loveservice/loveservice/internal/events"
	"github.com/loveservice/loveservice/internal/mailer"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/seed"
	"github.com/loveservice/loveservice/internal/service"
	"github.com/loveservice/loveservice/internal/storage"
	"github.com/loveservice/loveservice/internal/store"
	"github.com/loveservice/loveservice/internal/transport/httpapi"
	"github.com/loveservice/loveservice/internal/transport/ws"
)

// Container owns every long-lived dependency of the process. Construction is
// separated from lifecycle (Run/Shutdown) so the wiring stays readable and can
// be reused by tests or alternative transports.
type Container struct {
	Config    *config.Config
	Logger    *slog.Logger
	Version   string
	Store     *store.Store
	Cache     *cache.Cache
	Storage   storage.Storage
	Hub       *ws.Hub
	Bus       events.Bus
	Consumer  *events.Consumer
	Publisher *events.Publisher
	Handler   http.Handler
}

// NewContainer wires infrastructure, services and transports in dependency
// order. Failures close everything that was already opened.
func NewContainer(ctx context.Context, cfg *config.Config, log *slog.Logger, version string) (*Container, error) {
	st, err := store.New(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}

	c := cache.New(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, log)

	media, err := buildStorage(ctx, cfg, log)
	if err != nil {
		st.Close()
		c.Close()
		return nil, fmt.Errorf("storage: %w", err)
	}

	tokens := security.NewTokenManager(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	mail := mailer.NewWithConfig(mailer.Config{
		Host:      cfg.SMTPHost,
		Port:      cfg.SMTPPort,
		User:      cfg.SMTPUser,
		Password:  cfg.SMTPPassword,
		From:      cfg.SMTPFrom,
		TLSMode:   cfg.SMTPTLS,
		Timeout:   cfg.SMTPTimeout,
		OutboxDir: cfg.MailOutbox,
	}, log)
	hub := ws.NewHub(c, log)

	authSvc := service.NewAuthService(st, tokens, mail, cfg.Env, log)
	profileSvc := service.NewProfileService(st, c, media, cfg.MaxUploadMB, cfg.AutoApproveMedia, log)
	discoverySvc := service.NewDiscoveryService(st, c)
	swipeSvc := service.NewSwipeService(st, c, cfg.DailyLikeLimit, cfg.DailySuperlikeLimit, cfg.RewindWindow, log)
	chatSvc := service.NewChatService(st, c, hub, log)
	notificationSvc := service.NewNotificationService(st, hub, log)
	accountSvc := service.NewAccountService(st, c)
	adminSvc := service.NewAdminService(st, cfg.AdminRequireSecondApproval, log)
	wsHandler := ws.NewHandler(hub, tokens, st, chatSvc, cfg.CORSOrigins, log)

	if cfg.SeedDemo {
		if err := seed.Run(ctx, st, media, log); err != nil {
			log.Warn("demo seed failed", "error", err)
		}
	}

	bus, consumer := buildEventBus(cfg, notificationSvc, log)
	publisher := events.NewPublisher(st, bus, log)

	var mediaHandler http.Handler
	if cfg.StorageDriver != "s3" {
		mediaHandler = http.FileServer(http.Dir(cfg.MediaDir))
	}

	handler := httpapi.NewRouter(httpapi.Deps{
		Config: cfg, Logger: log, Store: st, Cache: c, Tokens: tokens,
		Auth: authSvc, Profiles: profileSvc, Discovery: discoverySvc, Swipes: swipeSvc,
		Chat: chatSvc, Notifications: notificationSvc, Account: accountSvc,
		Admin: adminSvc,
		WS:    wsHandler, MediaHandler: mediaHandler, Version: version,
	})

	return &Container{
		Config: cfg, Logger: log, Version: version,
		Store: st, Cache: c, Storage: media, Hub: hub,
		Bus: bus, Consumer: consumer, Publisher: publisher, Handler: handler,
	}, nil
}

// Close releases resources in reverse order of acquisition.
func (c *Container) Close() {
	if c.Consumer != nil {
		_ = c.Consumer.Close()
	}
	if c.Bus != nil {
		_ = c.Bus.Close()
	}
	if c.Cache != nil {
		_ = c.Cache.Close()
	}
	if c.Store != nil {
		c.Store.Close()
	}
}

func buildStorage(ctx context.Context, cfg *config.Config, log *slog.Logger) (storage.Storage, error) {
	if cfg.StorageDriver == "s3" {
		return storage.NewS3(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, cfg.S3UseSSL, cfg.S3PublicURL, log)
	}
	return storage.NewLocal(cfg.MediaDir, cfg.MediaBaseURL)
}

// buildEventBus prefers Kafka and degrades to the in-process bus when Kafka is
// disabled or unreachable, so local development never blocks on infrastructure.
func buildEventBus(cfg *config.Config, notificationSvc *service.NotificationService, log *slog.Logger) (events.Bus, *events.Consumer) {
	if cfg.KafkaEnabled && kafkaReachable(cfg.KafkaBrokers) {
		ensureKafkaTopic(cfg.KafkaBrokers, cfg.KafkaTopic, log)
		bus := events.NewKafkaBus(cfg.KafkaBrokers, cfg.KafkaTopic, log)
		consumer := events.NewConsumer(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaGroupID, notificationSvc.HandleEvent, log)
		log.Info("kafka event bus enabled", "brokers", cfg.KafkaBrokers, "topic", cfg.KafkaTopic)
		return bus, consumer
	}
	if cfg.KafkaEnabled {
		log.Warn("kafka is unreachable; falling back to the in-process event bus")
	}
	return events.NewLocalBus(notificationSvc.HandleEvent, log), nil
}

// ensureKafkaTopic waits briefly for the broker to accept a topic creation
// request. Kafka may answer the health check before it is ready to create
// topics, and a missing topic makes the first outbox publish fail.
func ensureKafkaTopic(brokers []string, topic string, log *slog.Logger) {
	if strings.TrimSpace(topic) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for attempt := 1; ; attempt++ {
		err := events.EnsureTopic(ctx, brokers, topic, 3)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			log.Warn("could not ensure kafka topic; events will retry from the outbox", "topic", topic, "error", err)
			return
		}
		log.Warn("ensuring kafka topic failed; retrying", "topic", topic, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			log.Warn("could not ensure kafka topic; events will retry from the outbox", "topic", topic, "error", ctx.Err())
			return
		case <-time.After(time.Second):
		}
	}
}

func kafkaReachable(brokers []string) bool {
	for _, broker := range brokers {
		conn, err := net.DialTimeout("tcp", broker, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}
