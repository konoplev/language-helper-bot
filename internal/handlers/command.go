package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"go-telegram-template/internal/flows"
	"go-telegram-template/pkg/models"
)

// CommandHandler handles Telegram bot commands (/start, /help, etc.).
type CommandHandler struct {
	tg       TelegramAPI
	flow     flows.Manager
	commands map[string]func(ctx context.Context, uc models.UpdateContext) error
	logger   *slog.Logger
}

func NewCommandHandler(tg TelegramAPI, flow flows.Manager, logger *slog.Logger) *CommandHandler {
	h := &CommandHandler{
		tg:       tg,
		flow:     flow,
		commands: make(map[string]func(ctx context.Context, uc models.UpdateContext) error),
		logger:   logger,
	}
	h.Register("start", h.handleStart)
	h.Register("help", h.handleHelp)
	return h
}

// Register adds a custom command handler.
func (h *CommandHandler) Register(cmd string, fn func(ctx context.Context, uc models.UpdateContext) error) {
	h.commands[cmd] = fn
}

func (h *CommandHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeCommand
}

func (h *CommandHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	cmd := uc.Update.Message.Command()
	fn, ok := h.commands[cmd]
	if !ok {
		_, err := h.tg.SendMessage(ctx, uc.ChatID, fmt.Sprintf("Unknown command: /%s", cmd))
		return err
	}
	return fn(ctx, uc)
}

// handleStart always shows the language picker so the user can (re-)select their
// preferred transcription language at the start of each session.
func (h *CommandHandler) handleStart(ctx context.Context, uc models.UpdateContext) error {
	kb := flows.LanguageKeyboard()
	_, err := h.tg.SendMessageWithKeyboard(ctx, uc.ChatID,
		"👋 Hello! I'm your voice transcription bot.\n\n🌍 Please choose your language:", kb)
	if err != nil {
		return err
	}
	if h.flow != nil {
		st := flows.NewUserState(uc.UserID, flows.FlowLanguage, flows.StateLanguageSelect)
		return h.flow.SetState(ctx, st)
	}
	return nil
}

func (h *CommandHandler) handleHelp(ctx context.Context, uc models.UpdateContext) error {
	text := "Available commands:\n/start — choose your language and get started\n/help — this help text\n\nSend a voice message to transcribe it."
	_, err := h.tg.SendMessage(ctx, uc.ChatID, text)
	return err
}
