package handlers

import (
	"context"

	"go-telegram-template/pkg/models"
)

// Handler routes and processes a Telegram update.
type Handler interface {
	CanHandle(uc models.UpdateContext) bool
	Handle(ctx context.Context, uc models.UpdateContext) error
}

// Dispatcher is implemented by the bot dispatcher; used for re-dispatching from callback handlers.
type Dispatcher interface {
	Dispatch(ctx context.Context, uc models.UpdateContext) error
}

// TelegramAPI abstracts outgoing Telegram API calls.
type TelegramAPI interface {
	SendMessage(ctx context.Context, chatID int64, text string) (int, error)
	SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, kb models.InlineKeyboardMarkup) (int, error)
	SendVoice(ctx context.Context, chatID int64, fileID string) (int, error)
	SendPhoto(ctx context.Context, chatID int64, fileID string, caption string) (int, error)
	EditMessageText(ctx context.Context, chatID int64, messageID int, text string) error
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
	GetFile(ctx context.Context, fileID string) (string, error)
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
	AnswerCallbackQuery(ctx context.Context, callbackID string) error
}

// Transcriber converts audio bytes to text.
// language is an ISO-639-1 code (e.g. "en", "ru"); empty string lets the
// service auto-detect.
type Transcriber interface {
	Transcribe(ctx context.Context, audioData []byte, filename string, language string) (string, error)
}

// PrefsStore persists per-user preferences such as the chosen transcription language.
type PrefsStore interface {
	Language(ctx context.Context, userID int64) (string, bool)
	SetLanguage(ctx context.Context, userID int64, lang string) error
}

// VoiceProcessor transcribes a voice message identified by its Telegram file_id
// and presents the draft to the user. Used by CallbackHandler to auto-process
// a pending voice after language selection.
type VoiceProcessor interface {
	ProcessVoice(ctx context.Context, chatID, userID int64, fileID, lang string) error
}
