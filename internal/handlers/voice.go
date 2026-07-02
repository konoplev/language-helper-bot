package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"deutsch-helper/internal/flows"
	"deutsch-helper/pkg/models"
)

// VoiceHandler transcribes voice messages and awaits the user's final text.
type VoiceHandler struct {
	tg          TelegramAPI
	transcriber Transcriber
	flow        flows.Manager
	prefs       PrefsStore
	logger      *slog.Logger
}

func NewVoiceHandler(tg TelegramAPI, transcriber Transcriber, flow flows.Manager, prefs PrefsStore, logger *slog.Logger) *VoiceHandler {
	return &VoiceHandler{tg: tg, transcriber: transcriber, flow: flow, prefs: prefs, logger: logger}
}

func (h *VoiceHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeVoice
}

func (h *VoiceHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	settings, ok := h.prefs.GetSettings(ctx, uc.UserID)
	if !ok || !settings.IsConfigured() {
		_, err := h.tg.SendMessage(ctx, uc.ChatID, "Please run /start first to configure your language settings.")
		return err
	}
	if settings.ActiveCommand == "" {
		_, err := h.tg.SendMessage(ctx, uc.ChatID, "Please select a command first: /from, /to, or /polish")
		return err
	}

	lang := transcriptionLanguage(settings)
	return h.processVoice(ctx, uc.ChatID, uc.UserID, uc.Update.Message.Voice.FileID, lang, settings.ActiveCommand)
}

// transcriptionLanguage returns the ISO 639-1 code to use for transcription.
// For /from the user speaks in their native language; for /to and /polish they speak in the learning language.
func transcriptionLanguage(s *models.UserSettings) string {
	if s.ActiveCommand == CmdFrom {
		return s.NativeLanguage
	}
	return s.LearningLanguage
}

func (h *VoiceHandler) processVoice(ctx context.Context, chatID, userID int64, fileID, lang, activeCommand string) error {
	h.logger.InfoContext(ctx, "processing voice message",
		slog.Int64("user_id", userID),
		slog.String("file_id", fileID),
		slog.String("lang", lang),
		slog.String("command", activeCommand),
	)

	filePath, err := h.tg.GetFile(ctx, fileID)
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}

	data, err := h.tg.DownloadFile(ctx, filePath)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	if _, err := h.tg.SendMessage(ctx, chatID, "Transcribing…"); err != nil {
		h.logger.WarnContext(ctx, "failed to send transcribing notice", slog.String("error", err.Error()))
	}

	text, err := h.transcriber.Transcribe(ctx, data, "voice.ogg", lang)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	h.logger.InfoContext(ctx, "transcription complete",
		slog.Int64("user_id", userID),
		slog.String("text", text),
	)

	// Save state so the next text message is treated as the final (possibly corrected) input.
	st := flows.NewUserState(userID, flows.FlowVoice, flows.StateVoicePending)
	st.Payload[flows.PayloadPendingTranscription] = text
	st.Payload[flows.PayloadActiveCommand] = activeCommand
	if err := h.flow.SetState(ctx, st); err != nil {
		return err
	}

	kb := models.NewInlineKeyboard(
		models.NewKeyboardRow(
			models.NewInlineQueryCurrentChatButton("Edit ✏️", text),
			models.NewCallbackButton("Send as is ✅", flows.CallbackVoiceSendText),
		),
	)
	_, err = h.tg.SendMessageWithKeyboard(ctx, chatID, "🎙 Transcription:\n\n"+text, kb)
	return err
}
