package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"deutsch-helper/internal/flows"
	"deutsch-helper/pkg/models"
)

// CallbackHandler processes inline keyboard button presses.
type CallbackHandler struct {
	tg         TelegramAPI
	flow       flows.Manager
	prefs      PrefsStore
	dispatcher Dispatcher
	logger     *slog.Logger
}

func NewCallbackHandler(tg TelegramAPI, flow flows.Manager, prefs PrefsStore, logger *slog.Logger) *CallbackHandler {
	return &CallbackHandler{tg: tg, flow: flow, prefs: prefs, logger: logger}
}

func (h *CallbackHandler) SetDispatcher(d Dispatcher) { h.dispatcher = d }

func (h *CallbackHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeCallbackQuery
}

func (h *CallbackHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	cq := uc.Update.CallbackQuery
	if err := h.tg.AnswerCallbackQuery(ctx, cq.ID); err != nil {
		h.logger.WarnContext(ctx, "answer callback query failed", slog.String("error", err.Error()))
	}

	switch {
	case strings.HasPrefix(cq.Data, flows.CallbackSetupNativePrefix):
		lang := strings.TrimPrefix(cq.Data, flows.CallbackSetupNativePrefix)
		return h.handleSetupNative(ctx, uc, lang)
	case strings.HasPrefix(cq.Data, flows.CallbackSetupLearningPrefix):
		lang := strings.TrimPrefix(cq.Data, flows.CallbackSetupLearningPrefix)
		return h.handleSetupLearning(ctx, uc, lang)
	case strings.HasPrefix(cq.Data, flows.CallbackSetupLevelPrefix):
		level := strings.TrimPrefix(cq.Data, flows.CallbackSetupLevelPrefix)
		return h.handleSetupLevel(ctx, uc, level)
	case cq.Data == flows.CallbackVoiceSendText:
		return h.handleVoiceSendAsIs(ctx, uc)
	default:
		h.logger.WarnContext(ctx, "unhandled callback", slog.String("data", cq.Data))
		return nil
	}
}

func (h *CallbackHandler) handleSetupNative(ctx context.Context, uc models.UpdateContext, lang string) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil {
		return err
	}
	if st == nil {
		st = flows.NewUserState(uc.UserID, flows.FlowSetup, flows.StateSetupLearning)
	} else {
		st.State = flows.StateSetupLearning
	}
	st.Payload[flows.PayloadSetupNative] = lang
	if err := h.flow.SetState(ctx, st); err != nil {
		return err
	}

	kb := flows.LearningLanguageKeyboard()
	_, err = h.tg.SendMessageWithKeyboard(ctx, uc.ChatID,
		fmt.Sprintf("Native language: %s ✓\n\nNow, what language are you learning?",
			models.LanguageName(lang)), kb)
	return err
}

func (h *CallbackHandler) handleSetupLearning(ctx context.Context, uc models.UpdateContext, lang string) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil {
		return err
	}
	if st == nil {
		st = flows.NewUserState(uc.UserID, flows.FlowSetup, flows.StateSetupLevel)
	} else {
		st.State = flows.StateSetupLevel
	}
	st.Payload[flows.PayloadSetupLearning] = lang
	if err := h.flow.SetState(ctx, st); err != nil {
		return err
	}

	kb := flows.LevelKeyboard()
	_, err = h.tg.SendMessageWithKeyboard(ctx, uc.ChatID,
		fmt.Sprintf("Learning: %s ✓\n\nWhat is your current level?",
			models.LanguageName(lang)), kb)
	return err
}

func (h *CallbackHandler) handleSetupLevel(ctx context.Context, uc models.UpdateContext, level string) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil {
		return err
	}

	var nativeLang, learningLang string
	if st != nil {
		nativeLang, _ = st.Payload[flows.PayloadSetupNative].(string)
		learningLang, _ = st.Payload[flows.PayloadSetupLearning].(string)
	}

	// Preserve ActiveCommand if user is updating settings via /start.
	existing, _ := h.prefs.GetSettings(ctx, uc.UserID)
	var activeCmd string
	if existing != nil {
		activeCmd = existing.ActiveCommand
	}

	settings := &models.UserSettings{
		NativeLanguage:   nativeLang,
		LearningLanguage: learningLang,
		Level:            level,
		ActiveCommand:    activeCmd,
	}
	if err := h.prefs.SaveSettings(ctx, uc.UserID, settings); err != nil {
		return err
	}
	if err := h.flow.ClearState(ctx, uc.UserID); err != nil {
		return err
	}

	h.logger.InfoContext(ctx, "setup complete",
		slog.Int64("user_id", uc.UserID),
		slog.String("native", nativeLang),
		slog.String("learning", learningLang),
		slog.String("level", level),
	)

	_, err = h.tg.SendMessage(ctx, uc.ChatID, fmt.Sprintf(
		"✅ Setup complete!\n\n"+
			"Native language: %s\n"+
			"Learning: %s\n"+
			"Level: %s\n\n"+
			"Commands:\n"+
			"/from — translate from %s to %s\n"+
			"/to — translate from %s to %s\n"+
			"/polish — grammar check your %s text",
		models.LanguageName(nativeLang),
		models.LanguageName(learningLang),
		level,
		models.LanguageName(nativeLang), models.LanguageName(learningLang),
		models.LanguageName(learningLang), models.LanguageName(nativeLang),
		models.LanguageName(learningLang),
	))
	return err
}

// handleVoiceSendAsIs re-dispatches the stored transcription as a synthetic text update.
func (h *CallbackHandler) handleVoiceSendAsIs(ctx context.Context, uc models.UpdateContext) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	transcription, _ := st.Payload[flows.PayloadPendingTranscription].(string)

	if err := h.flow.ClearState(ctx, uc.UserID); err != nil {
		return err
	}

	// Synthesize a text message update so the text handler processes the transcription.
	cq := uc.Update.CallbackQuery
	msg := *cq.Message
	msg.Text = transcription
	msg.From = cq.From
	msg.Voice = nil
	msg.Entities = nil

	return h.dispatcher.Dispatch(ctx, models.NewUpdateContext(tgbotapi.Update{Message: &msg}))
}
