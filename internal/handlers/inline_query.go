package handlers

import (
	"context"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"deutsch-helper/internal/flows"
	"deutsch-helper/pkg/models"
)

// InlineQueryHandler handles inline query updates produced when the user edits
// a transcription via the switch_inline_query_current_chat button.
type InlineQueryHandler struct {
	tg         TelegramAPI
	flow       flows.Manager
	dispatcher Dispatcher
	logger     *slog.Logger
}

func NewInlineQueryHandler(tg TelegramAPI, flow flows.Manager, logger *slog.Logger) *InlineQueryHandler {
	return &InlineQueryHandler{tg: tg, flow: flow, logger: logger}
}

func (h *InlineQueryHandler) SetDispatcher(d Dispatcher) { h.dispatcher = d }

func (h *InlineQueryHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeInlineQuery
}

func (h *InlineQueryHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	iq := uc.Update.InlineQuery

	// Dismiss the inline spinner immediately; we handle this as a regular text flow.
	if err := h.tg.AnswerInlineQuery(ctx, iq.ID); err != nil {
		h.logger.WarnContext(ctx, "answer inline query failed", slog.String("error", err.Error()))
	}

	text := stripBotMention(iq.Query)
	if text == "" {
		return nil
	}

	if err := h.flow.ClearState(ctx, uc.UserID); err != nil {
		return err
	}

	// Synthesize a plain text update so the text handler processes it normally.
	msg := &tgbotapi.Message{
		Text: text,
		From: iq.From,
		Chat: &tgbotapi.Chat{ID: uc.ChatID},
	}
	return h.dispatcher.Dispatch(ctx, models.NewUpdateContext(tgbotapi.Update{Message: msg}))
}

// stripBotMention removes the leading "@botname " that Telegram prepends when
// switch_inline_query_current_chat populates the input box.
func stripBotMention(query string) string {
	if strings.HasPrefix(query, "@") {
		if idx := strings.Index(query, " "); idx != -1 {
			return query[idx+1:]
		}
		return ""
	}
	return query
}
