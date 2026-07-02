package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"deutsch-helper/internal/flows"
	"deutsch-helper/pkg/models"
)

// TextHandler processes plain text messages:
//   - Confirms voice transcriptions (voice/pending state)
//   - Runs the active command via the AI processor
type TextHandler struct {
	tg     TelegramAPI
	flow   flows.Manager
	prefs  PrefsStore
	ai     TextProcessor
	logger *slog.Logger
}

func NewTextHandler(tg TelegramAPI, flow flows.Manager, prefs PrefsStore, ai TextProcessor, logger *slog.Logger) *TextHandler {
	return &TextHandler{tg: tg, flow: flow, prefs: prefs, ai: ai, logger: logger}
}

func (h *TextHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeText
}

func (h *TextHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	// If the user is in a pending voice state, treat the incoming text as the final input.
	if ok, err := h.flow.IsInState(ctx, uc.UserID, flows.FlowVoice, flows.StateVoicePending); err != nil {
		return err
	} else if ok {
		return h.handleVoiceConfirmation(ctx, uc)
	}

	// Text during setup: prompt to use the buttons.
	inSetup, err := h.flow.IsInFlow(ctx, uc.UserID, flows.FlowSetup)
	if err != nil {
		return err
	}
	if inSetup {
		_, err = h.tg.SendMessage(ctx, uc.ChatID, "Please use the buttons above to make your selection.")
		return err
	}

	settings, ok := h.prefs.GetSettings(ctx, uc.UserID)
	if !ok || !settings.IsConfigured() {
		_, err = h.tg.SendMessage(ctx, uc.ChatID, "Please run /start to configure your language settings first.")
		return err
	}

	if settings.ActiveCommand == "" {
		_, err = h.tg.SendMessage(ctx, uc.ChatID, "Please select a command: /from, /to, or /polish")
		return err
	}

	return h.processText(ctx, uc, settings, uc.Update.Message.Text)
}

// handleVoiceConfirmation takes the user's text as the final input for the voice command.
func (h *TextHandler) handleVoiceConfirmation(ctx context.Context, uc models.UpdateContext) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil {
		return err
	}
	activeCommand, _ := st.Payload[flows.PayloadActiveCommand].(string)

	if err := h.flow.ClearState(ctx, uc.UserID); err != nil {
		return err
	}

	settings, ok := h.prefs.GetSettings(ctx, uc.UserID)
	if !ok || !settings.IsConfigured() {
		_, err = h.tg.SendMessage(ctx, uc.ChatID, "Settings not found. Please run /start again.")
		return err
	}
	settings.ActiveCommand = activeCommand
	return h.processText(ctx, uc, settings, stripBotMention(uc.Update.Message.Text))
}

// processText calls the AI with the appropriate prompt and sends the result back.
func (h *TextHandler) processText(ctx context.Context, uc models.UpdateContext, settings *models.UserSettings, text string) error {
	h.logger.InfoContext(ctx, "processing text",
		slog.Int64("user_id", uc.UserID),
		slog.String("command", settings.ActiveCommand),
	)

	systemPrompt := buildPrompt(settings)
	result, err := h.ai.Complete(ctx, systemPrompt, text)
	if err != nil {
		h.logger.ErrorContext(ctx, "ai completion failed", slog.String("error", err.Error()))
		_, sendErr := h.tg.SendMessage(ctx, uc.ChatID, "Sorry, something went wrong. Please try again.")
		if sendErr != nil {
			return sendErr
		}
		return err
	}

	_, err = h.tg.SendMessage(ctx, uc.ChatID, result)
	return err
}

func buildPrompt(s *models.UserSettings) string {
	native := models.LanguageName(s.NativeLanguage)
	learning := models.LanguageName(s.LearningLanguage)

	switch s.ActiveCommand {
	case CmdFrom:
		return fmt.Sprintf(
			"Translate the following text from %s to %s. "+
				"The translation must be appropriate for CEFR level %s. "+
				"Respond with ONLY the translation — no explanations, no extra text.",
			native, learning, s.Level)

	case CmdTo:
		return fmt.Sprintf(
			"Translate the following text from %s to %s. "+
				"Respond with ONLY the translation — no explanations, no extra text.",
			learning, native)

	case CmdPolish:
		return fmt.Sprintf(
			"You are a language teacher. The student is learning %s at CEFR level %s. "+
				"Their native language is %s.\n\n"+
				"Analyze the student's text written in %s and provide your response in this exact structure:\n\n"+
				"Original:\n[copy the original text here]\n\n"+
				"Mistakes:\n[numbered list of mistakes — grammar, spelling, word choice, style]\n\n"+
				"Explanations:\n[for each mistake: explain in %s, state the grammar rule being violated, "+
				"include a corrected example]\n\n"+
				"Improved version:\n[corrected text that is as close as possible to the original style]",
			learning, s.Level, native, learning, native)

	default:
		return ""
	}
}
