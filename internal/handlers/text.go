package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"go-telegram-template/internal/flows"
	"go-telegram-template/pkg/models"
)

// TextHandler processes plain text messages, including draft editing in the voice flow.
type TextHandler struct {
	tg     TelegramAPI
	flow   flows.Manager
	logger *slog.Logger
}

func NewTextHandler(tg TelegramAPI, flow flows.Manager, logger *slog.Logger) *TextHandler {
	return &TextHandler{tg: tg, flow: flow, logger: logger}
}

func (h *TextHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeText
}

func (h *TextHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	inEdit, err := h.flow.IsInState(ctx, uc.UserID, flows.FlowVoice, flows.StateVoiceEdit)
	if err != nil {
		return err
	}
	if inEdit {
		return h.handleEdit(ctx, uc)
	}

	// Default echo — replace with your application logic.
	_, err = h.tg.SendMessage(ctx, uc.ChatID, fmt.Sprintf("You said: %s", uc.Update.Message.Text))
	return err
}

func (h *TextHandler) handleEdit(ctx context.Context, uc models.UpdateContext) error {
	st, err := h.flow.GetState(ctx, uc.UserID)
	if err != nil {
		return err
	}

	newText := uc.Update.Message.Text
	st.State = flows.StateVoiceDraft
	st.Payload[flows.PayloadDraftText] = newText

	if err := h.flow.SetState(ctx, st); err != nil {
		return err
	}

	kb := models.NewInlineKeyboard(
		models.NewKeyboardRow(
			models.NewCallbackButton("Edit", flows.CallbackVoiceEdit),
			models.NewCallbackButton("Send as text", flows.CallbackVoiceSendText),
			models.NewCallbackButton("Send as command", flows.CallbackVoiceSendCommand),
		),
	)
	msgID, err := h.tg.SendMessageWithKeyboard(ctx, uc.ChatID, newText, kb)
	if err != nil {
		return err
	}
	st.Payload[flows.PayloadDraftMsgID] = msgID
	return h.flow.SetState(ctx, st)
}
