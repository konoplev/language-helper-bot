package bot

import (
	"context"
	"log/slog"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"deutsch-helper/internal/flows"
	"deutsch-helper/internal/handlers"
	"deutsch-helper/internal/middleware"
	"deutsch-helper/internal/services/telegram"
	"deutsch-helper/pkg/models"
)

const (
	PollingModeLong    = "long_polling"
	PollingModeWebhook = "webhook"
)

// Bot ties together the update source, dispatcher, and all handlers.
type Bot struct {
	api         *tgbotapi.BotAPI
	tgClient    *telegram.Client
	dispatcher  *Dispatcher
	pollingMode string
	webhookAddr string
	logger      *slog.Logger
}

// Option configures the Bot.
type Option func(*Bot)

// WithWebhookAddr sets the webhook listen address (e.g. ":8080").
func WithWebhookAddr(addr string) Option {
	return func(b *Bot) { b.webhookAddr = addr }
}

// Dependencies bundles all external clients needed by the bot.
type Dependencies struct {
	TelegramClient *telegram.Client
	Transcriber    handlers.Transcriber
	AI             handlers.TextProcessor
	Prefs          handlers.PrefsStore
	AllowedUsers   []int64
}

// New constructs and wires the entire bot.
func New(token string, pollingMode string, deps Dependencies, logger *slog.Logger, opts ...Option) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	flowEngine := flows.NewEngine(flows.NewInMemoryStorage())
	dispatcher := NewDispatcher(logger)

	cmdHandler := handlers.NewCommandHandler(deps.TelegramClient, flowEngine, deps.Prefs, logger)
	voiceHandler := handlers.NewVoiceHandler(deps.TelegramClient, deps.Transcriber, flowEngine, deps.Prefs, logger)
	textHandler := handlers.NewTextHandler(deps.TelegramClient, flowEngine, deps.Prefs, deps.AI, logger)
	callbackHandler := handlers.NewCallbackHandler(deps.TelegramClient, flowEngine, deps.Prefs, logger)
	callbackHandler.SetDispatcher(dispatcher)

	// Register in priority order: callbacks first, then commands, voice, text.
	dispatcher.Register(callbackHandler)
	dispatcher.Register(cmdHandler)
	dispatcher.Register(voiceHandler)
	dispatcher.Register(textHandler)

	// Build middleware stack; allowlist is outermost so unauthorized users are blocked first.
	mws := []middleware.Middleware{
		middleware.Recovery(logger),
		middleware.Timing(45 * time.Second),
		middleware.Logging(logger),
	}
	if len(deps.AllowedUsers) > 0 {
		al := middleware.NewAllowlist(deps.AllowedUsers, deps.TelegramClient, logger)
		mws = append([]middleware.Middleware{al.Middleware()}, mws...)
	}
	dispatcher.Use(mws...)

	b := &Bot{
		api:         api,
		tgClient:    deps.TelegramClient,
		dispatcher:  dispatcher,
		pollingMode: pollingMode,
		webhookAddr: ":8080",
		logger:      logger,
	}
	for _, o := range opts {
		o(b)
	}
	return b, nil
}

// Run registers bot commands with Telegram and starts polling/webhook.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.tgClient.SetMyCommands(ctx, botCommands()); err != nil {
		b.logger.Warn("failed to register bot commands", slog.String("error", err.Error()))
	}

	switch b.pollingMode {
	case PollingModeWebhook:
		b.logger.Info("starting in webhook mode", slog.String("addr", b.webhookAddr))
		return b.runWebhook(ctx, b.webhookAddr)
	default:
		b.logger.Info("starting in long-polling mode", slog.String("username", b.api.Self.UserName))
		return b.runPolling(ctx)
	}
}

func (b *Bot) runPolling(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return ctx.Err()
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			uc := models.NewUpdateContext(update)
			if err := b.dispatcher.Dispatch(ctx, uc); err != nil {
				b.logger.ErrorContext(ctx, "dispatch error", slog.Any("error", err))
			}
		}
	}
}

func botCommands() []telegram.BotCommand {
	return []telegram.BotCommand{
		{Command: "start", Description: "Configure your language pair and level"},
		{Command: "from", Description: "Translate from your native language"},
		{Command: "to", Description: "Translate to your native language"},
		{Command: "polish", Description: "Grammar check and improve your text"},
	}
}
