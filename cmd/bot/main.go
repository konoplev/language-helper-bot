package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"deutsch-helper/internal/bot"
	"deutsch-helper/internal/middleware"
	"deutsch-helper/internal/services/groq"
	"deutsch-helper/internal/services/openai"
	"deutsch-helper/internal/services/telegram"
	"deutsch-helper/internal/store"
	"deutsch-helper/pkg/models"
)

func main() {
	cfg, err := models.LoadConfig()
	if err != nil {
		slog.Error("config error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	allowedUsers, err := middleware.LoadAllowedUsers(cfg.AllowedUsersFile)
	if err != nil {
		logger.Error("failed to load allowed users", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("loaded allowed users", slog.Int("count", len(allowedUsers)))

	prefs, err := store.NewFilePrefsStore(cfg.PrefsFile)
	if err != nil {
		logger.Error("failed to load prefs store", slog.String("error", err.Error()))
		os.Exit(1)
	}

	tgClient := telegram.NewClient(cfg.BotToken)
	groqClient := groq.NewClient(cfg.GroqAPIKey)
	openAIClient := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)

	deps := bot.Dependencies{
		TelegramClient: tgClient,
		Transcriber:    groqClient,
		AI:             openAIClient,
		Prefs:          prefs,
		AllowedUsers:   allowedUsers,
	}

	b, err := bot.New(cfg.BotToken, cfg.PollingMode, deps, logger)
	if err != nil {
		logger.Error("failed to create bot", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := b.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("bot exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("bot stopped")
}
