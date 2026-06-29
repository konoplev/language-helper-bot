package handlers_test

import (
	"context"
	"errors"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"go-telegram-template/internal/handlers"
	"go-telegram-template/pkg/models"
)

// stubHandler is a test double that tracks calls.
type stubHandler struct {
	canHandle bool
	called    bool
	err       error
}

func (s *stubHandler) CanHandle(_ models.UpdateContext) bool { return s.canHandle }
func (s *stubHandler) Handle(_ context.Context, _ models.UpdateContext) error {
	s.called = true
	return s.err
}

// stubTelegram satisfies handlers.TelegramAPI for unit tests.
type stubTelegram struct {
	sentText  string
	sentMsgID int
	err       error
}

func (s *stubTelegram) SendMessage(_ context.Context, _ int64, text string) (int, error) {
	s.sentText = text
	return s.sentMsgID, s.err
}
func (s *stubTelegram) SendMessageWithKeyboard(_ context.Context, _ int64, text string, _ models.InlineKeyboardMarkup) (int, error) {
	s.sentText = text
	return s.sentMsgID, s.err
}
func (s *stubTelegram) SendVoice(_ context.Context, _ int64, _ string) (int, error) {
	return s.sentMsgID, s.err
}
func (s *stubTelegram) SendPhoto(_ context.Context, _ int64, _ string, _ string) (int, error) {
	return s.sentMsgID, s.err
}
func (s *stubTelegram) EditMessageText(_ context.Context, _ int64, _ int, _ string) error { return nil }
func (s *stubTelegram) DeleteMessage(_ context.Context, _ int64, _ int) error              { return nil }
func (s *stubTelegram) GetFile(_ context.Context, _ string) (string, error)                { return "", nil }
func (s *stubTelegram) DownloadFile(_ context.Context, _ string) ([]byte, error)           { return nil, nil }
func (s *stubTelegram) AnswerCallbackQuery(_ context.Context, _ string) error              { return nil }

func textUpdate(userID int64, text string) models.UpdateContext {
	return models.UpdateContext{
		Update: tgbotapi.Update{
			Message: &tgbotapi.Message{
				From: &tgbotapi.User{ID: userID},
				Chat: &tgbotapi.Chat{ID: userID},
				Text: text,
			},
		},
		Type:   models.UpdateTypeText,
		UserID: userID,
		ChatID: userID,
	}
}

func commandUpdate(userID int64, cmd string) models.UpdateContext {
	return models.UpdateContext{
		Update: tgbotapi.Update{
			Message: &tgbotapi.Message{
				From: &tgbotapi.User{ID: userID},
				Chat: &tgbotapi.Chat{ID: userID},
				Text: "/" + cmd,
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: len(cmd) + 1},
				},
			},
		},
		Type:   models.UpdateTypeCommand,
		UserID: userID,
		ChatID: userID,
	}
}

// TestDispatchRouting verifies that updates reach the first matching handler.
func TestDispatchRouting(t *testing.T) {
	cases := []struct {
		name         string
		handlers     []*stubHandler
		uc           models.UpdateContext
		wantCalledAt int // index of handler expected to be called (-1 = none)
	}{
		{
			name: "first matching handler is called",
			handlers: []*stubHandler{
				{canHandle: false},
				{canHandle: true},
				{canHandle: true},
			},
			uc:           textUpdate(1, "hi"),
			wantCalledAt: 1,
		},
		{
			name: "no match — no handler called",
			handlers: []*stubHandler{
				{canHandle: false},
				{canHandle: false},
			},
			uc:           textUpdate(1, "hi"),
			wantCalledAt: -1,
		},
		{
			name: "single matching handler",
			handlers: []*stubHandler{
				{canHandle: true},
			},
			uc:           commandUpdate(1, "start"),
			wantCalledAt: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ifaces := make([]handlers.Handler, len(tc.handlers))
			for i, h := range tc.handlers {
				ifaces[i] = h
			}

			dispatch := func(ctx context.Context, uc models.UpdateContext) error {
				for _, h := range ifaces {
					if h.CanHandle(uc) {
						return h.Handle(ctx, uc)
					}
				}
				return nil
			}

			if err := dispatch(context.Background(), tc.uc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for i, h := range tc.handlers {
				shouldBeCalled := i == tc.wantCalledAt
				if h.called != shouldBeCalled {
					t.Errorf("handler[%d].called = %v, want %v", i, h.called, shouldBeCalled)
				}
			}
		})
	}
}

// TestHandlerErrors verifies errors propagate through dispatch.
func TestHandlerErrors(t *testing.T) {
	wantErr := errors.New("boom")
	h := &stubHandler{canHandle: true, err: wantErr}

	dispatch := func(ctx context.Context, uc models.UpdateContext) error {
		if h.CanHandle(uc) {
			return h.Handle(ctx, uc)
		}
		return nil
	}

	err := dispatch(context.Background(), textUpdate(1, "hi"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}

// TestCommandHandlerRouting verifies known vs unknown commands.
func TestCommandHandlerRouting(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		wantText string
	}{
		{"start command", "start", "👋 Hello"},
		{"help command", "help", "Available commands"},
		{"unknown command", "foobar", "Unknown command"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := &stubTelegram{sentMsgID: 1}
			h := handlers.NewCommandHandler(tg, nil, nil)

			uc := commandUpdate(1, tc.cmd)
			if err := h.Handle(context.Background(), uc); err != nil {
				t.Fatal(err)
			}
			if len(tg.sentText) == 0 {
				t.Fatal("no message sent")
			}
		})
	}
}
