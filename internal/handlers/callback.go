package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"go-telegram-template/internal/flows"
	"go-telegram-template/pkg/models"
)

// CallbackHandler processes inline keyboard button presses for the voice draft
// flow and the language-selection flow.
type CallbackHandler struct {
	tg         TelegramAPI
	flow       flows.Manager
	prefs      PrefsStore
	dispatcher Dispatcher
	voiceProc  VoiceProcessor
	logger     *slog.Logger
}

func NewCallbackHandler(tg TelegramAPI, flow flows.Manager, prefs PrefsStore, logger *slog.Logger) *CallbackHandler {
	return &CallbackHandler{tg: tg, flow: flow, prefs: prefs, logger: logger}
}

// SetDispatcher wires the bot dispatcher after construction to avoid circular init.
func (h *CallbackHandler) SetDispatcher(d Dispatcher) { h.dispatcher = d }

// SetVoiceProcessor wires the VoiceProcessor so language-selection can auto-transcribe
// a queued voice message.
func (h *CallbackHandler) SetVoiceProcessor(vp VoiceProcessor) { h.voiceProc = vp }

func (h *CallbackHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeCallbackQuery
}

func (h *CallbackHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	cq := uc.Update.CallbackQuery
	if err := h.tg.AnswerCallbackQuery(ctx, cq.ID); err != nil {
		h.logger.WarnContext(ctx, "answer callback query failed", slog.String("error", err.Error()))
	}

	switch {
	case cq.Data == flows.CallbackVoiceEdit:
		return h.handleEdit(ctx, uc)
	case cq.Data == flows.CallbackVoiceSendText:
		return h.handleSendText(ctx, uc)
	case cq.Data == flows.CallbackVoiceSendCommand:
		return h.handleSendCommand(ctx, uc)
	case strings.HasPrefix(cq.Data, flows.CallbackLangPrefix):
		lang := strings.TrimPrefix(cq.Data, flows.CallbackLangPrefix)
		return h.handleLanguageSelect(ctx, uc, lang)
	default:
		h.logger.WarnContext(ctx, "unhandled callback", slog.String("data", cq.Data))
		return nil
	}
}

func (h *CallbackHandler) handleEdit(ctx context.Context, uc models.UpdateContext) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil || st == nil {
		return err
	}
	st.State = flows.StateVoiceEdit
	if err := h.flow.SetState(ctx, st); err != nil {
		return err
	}
	_, err = h.tg.SendMessage(ctx, uc.ChatID, "Please type your correction:")
	return err
}

func (h *CallbackHandler) handleSendText(ctx context.Context, uc models.UpdateContext) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil || st == nil {
		return err
	}
	text, _ := st.Payload[flows.PayloadDraftText].(string)
	if err := h.flow.ClearState(ctx, uc.UserID); err != nil {
		return err
	}
	_, err = h.tg.SendMessage(ctx, uc.ChatID, text)
	return err
}

func (h *CallbackHandler) handleSendCommand(ctx context.Context, uc models.UpdateContext) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil || st == nil {
		return err
	}
	text, _ := st.Payload[flows.PayloadDraftText].(string)
	if err := h.flow.ClearState(ctx, uc.UserID); err != nil {
		return err
	}

	if h.dispatcher == nil {
		_, err = h.tg.SendMessage(ctx, uc.ChatID, fmt.Sprintf("Command: %s", text))
		return err
	}

	// Synthesise a command update from the transcribed text.
	cmd := strings.TrimPrefix(strings.Fields(text)[0], "/")
	syntheticMsg := *uc.Update.CallbackQuery.Message
	syntheticMsg.Text = "/" + cmd
	syntheticMsg.Entities = []tgbotapi.MessageEntity{{
		Type:   "bot_command",
		Offset: 0,
		Length: len(cmd) + 1,
	}}
	syntheticMsg.From = uc.Update.CallbackQuery.From

	synthetic := tgbotapi.Update{Message: &syntheticMsg}
	return h.dispatcher.Dispatch(ctx, models.NewUpdateContext(synthetic))
}

func (h *CallbackHandler) handleLanguageSelect(ctx context.Context, uc models.UpdateContext, lang string) error {
	if err := h.prefs.SetLanguage(ctx, uc.UserID, lang); err != nil {
		return err
	}

	label := flows.LanguageLabel(lang)
	h.logger.InfoContext(ctx, "language selected",
		slog.Int64("user_id", uc.UserID),
		slog.String("lang", lang),
		slog.String("label", label),
	)

	// Check for a pending voice message saved when the language wasn't known yet.
	st, err := h.flow.GetState(ctx, uc.UserID)
	var pendingFileID string
	if err == nil && st != nil && st.Flow == flows.FlowLanguage {
		pendingFileID, _ = st.Payload[flows.PayloadPendingVoiceID].(string)
		if clearErr := h.flow.ClearState(ctx, uc.UserID); clearErr != nil {
			return clearErr
		}
	}

	if pendingFileID != "" && h.voiceProc != nil {
		notice := fmt.Sprintf("Language set to %s. Transcribing your voice message…", label)
		if _, err := h.tg.SendMessage(ctx, uc.ChatID, notice); err != nil {
			h.logger.WarnContext(ctx, "send language confirmation failed", slog.String("error", err.Error()))
		}
		return h.voiceProc.ProcessVoice(ctx, uc.ChatID, uc.UserID, pendingFileID, lang)
	}

	_, err = h.tg.SendMessage(ctx, uc.ChatID,
		fmt.Sprintf("Language set to %s! Send me a voice message and I'll transcribe it.", label))
	return err
}
