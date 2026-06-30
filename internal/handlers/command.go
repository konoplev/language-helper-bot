package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"deutsch-helper/internal/flows"
	"deutsch-helper/pkg/models"
)

const (
	CmdFrom   = "from"
	CmdTo     = "to"
	CmdPolish = "polish"
)

// CommandHandler handles bot commands (/start, /from, /to, /polish).
type CommandHandler struct {
	tg       TelegramAPI
	flow     flows.Manager
	prefs    PrefsStore
	commands map[string]func(ctx context.Context, uc models.UpdateContext) error
	logger   *slog.Logger
}

func NewCommandHandler(tg TelegramAPI, flow flows.Manager, prefs PrefsStore, logger *slog.Logger) *CommandHandler {
	h := &CommandHandler{
		tg:       tg,
		flow:     flow,
		prefs:    prefs,
		commands: make(map[string]func(ctx context.Context, uc models.UpdateContext) error),
		logger:   logger,
	}
	h.commands["start"] = h.handleStart
	h.commands["from"] = h.handleFrom
	h.commands["to"] = h.handleTo
	h.commands["polish"] = h.handlePolish
	return h
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

// handleStart begins the setup wizard: native language → learning language → level.
func (h *CommandHandler) handleStart(ctx context.Context, uc models.UpdateContext) error {
	if h.flow != nil {
		st := flows.NewUserState(uc.UserID, flows.FlowSetup, flows.StateSetupNative)
		if err := h.flow.SetState(ctx, st); err != nil {
			return err
		}
	}
	kb := flows.NativeLanguageKeyboard()
	_, err := h.tg.SendMessageWithKeyboard(ctx, uc.ChatID,
		"👋 Welcome! Let's set up your language learning assistant.\n\nWhat is your native language?", kb)
	return err
}

func (h *CommandHandler) handleFrom(ctx context.Context, uc models.UpdateContext) error {
	return h.activateCommand(ctx, uc, CmdFrom)
}

func (h *CommandHandler) handleTo(ctx context.Context, uc models.UpdateContext) error {
	return h.activateCommand(ctx, uc, CmdTo)
}

func (h *CommandHandler) handlePolish(ctx context.Context, uc models.UpdateContext) error {
	return h.activateCommand(ctx, uc, CmdPolish)
}

func (h *CommandHandler) activateCommand(ctx context.Context, uc models.UpdateContext, cmd string) error {
	if h.prefs == nil {
		_, err := h.tg.SendMessage(ctx, uc.ChatID, "Please run /start first to configure your language settings.")
		return err
	}
	settings, ok := h.prefs.GetSettings(ctx, uc.UserID)
	if !ok || !settings.IsConfigured() {
		_, err := h.tg.SendMessage(ctx, uc.ChatID, "Please run /start first to configure your language settings.")
		return err
	}
	settings.ActiveCommand = cmd
	if err := h.prefs.SaveSettings(ctx, uc.UserID, settings); err != nil {
		return err
	}
	// Clear any stale voice flow state.
	if h.flow != nil {
		_ = h.flow.ClearState(ctx, uc.UserID)
	}

	var prompt string
	switch cmd {
	case CmdFrom:
		prompt = fmt.Sprintf("✅ Mode: Translate %s → %s (%s level)\n\nSend text or voice in %s:",
			models.LanguageName(settings.NativeLanguage),
			models.LanguageName(settings.LearningLanguage),
			settings.Level,
			models.LanguageName(settings.NativeLanguage))
	case CmdTo:
		prompt = fmt.Sprintf("✅ Mode: Translate %s → %s\n\nSend text or voice in %s:",
			models.LanguageName(settings.LearningLanguage),
			models.LanguageName(settings.NativeLanguage),
			models.LanguageName(settings.LearningLanguage))
	case CmdPolish:
		prompt = fmt.Sprintf("✅ Mode: Grammar check & correction (%s, %s level)\n\nSend text or voice in %s:",
			models.LanguageName(settings.LearningLanguage),
			settings.Level,
			models.LanguageName(settings.LearningLanguage))
	}
	_, err := h.tg.SendMessage(ctx, uc.ChatID, prompt)
	return err
}
