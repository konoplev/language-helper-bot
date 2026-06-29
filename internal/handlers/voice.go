package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"go-telegram-template/internal/flows"
	"go-telegram-template/pkg/models"
)

// VoiceHandler downloads a Telegram voice message, transcribes it via Groq,
// and presents a draft to the user with editing options.
// If the user has not yet selected a language, it asks them first and queues the
// voice message for automatic transcription once a language is chosen.
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
	lang, ok := h.prefs.Language(ctx, uc.UserID)
	if !ok {
		return h.requestLanguage(ctx, uc)
	}
	return h.ProcessVoice(ctx, uc.ChatID, uc.UserID, uc.Update.Message.Voice.FileID, lang)
}

// ProcessVoice implements VoiceProcessor: downloads a voice message by file_id,
// transcribes it in the given language, and sends a draft with an inline keyboard.
func (h *VoiceHandler) ProcessVoice(ctx context.Context, chatID, userID int64, fileID, lang string) error {
	h.logger.InfoContext(ctx, "processing voice message",
		slog.Int64("user_id", userID),
		slog.String("file_id", fileID),
		slog.String("lang", lang),
	)

	filePath, err := h.tg.GetFile(ctx, fileID)
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}
	h.logger.DebugContext(ctx, "voice file path resolved", slog.String("file_path", filePath))

	data, err := h.tg.DownloadFile(ctx, filePath)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}
	h.logger.DebugContext(ctx, "voice file downloaded",
		slog.String("file_path", filePath),
		slog.Int("bytes", len(data)),
	)

	_, err = h.tg.SendMessage(ctx, chatID, "Transcribing…")
	if err != nil {
		h.logger.WarnContext(ctx, "failed to send transcribing notice", slog.String("error", err.Error()))
	}

	text, err := h.transcriber.Transcribe(ctx, data, "voice.ogg", lang)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	h.logger.InfoContext(ctx, "transcription complete",
		slog.Int64("user_id", userID),
		slog.Int("text_len", len(text)),
		slog.String("text", text),
	)

	kb := models.NewInlineKeyboard(
		models.NewKeyboardRow(
			models.NewCallbackButton("Edit", flows.CallbackVoiceEdit),
			models.NewCallbackButton("Send as text", flows.CallbackVoiceSendText),
			models.NewCallbackButton("Send as command", flows.CallbackVoiceSendCommand),
		),
	)

	msgID, err := h.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)
	if err != nil {
		return fmt.Errorf("send draft: %w", err)
	}

	st := flows.NewUserState(userID, flows.FlowVoice, flows.StateVoiceDraft)
	st.Payload[flows.PayloadDraftText] = text
	st.Payload[flows.PayloadDraftMsgID] = msgID
	return h.flow.SetState(ctx, st)
}

// requestLanguage saves the pending voice file_id and asks the user to pick a language.
// CallbackHandler will auto-transcribe the pending voice once a language is chosen.
func (h *VoiceHandler) requestLanguage(ctx context.Context, uc models.UpdateContext) error {
	st := flows.NewUserState(uc.UserID, flows.FlowLanguage, flows.StateLanguageSelect)
	st.Payload[flows.PayloadPendingVoiceID] = uc.Update.Message.Voice.FileID
	if err := h.flow.SetState(ctx, st); err != nil {
		return err
	}
	kb := flows.LanguageKeyboard()
	_, err := h.tg.SendMessageWithKeyboard(ctx, uc.ChatID,
		"🌍 Please choose your language for voice transcription:", kb)
	return err
}
