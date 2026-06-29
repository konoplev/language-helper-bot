package telegram

import "go-telegram-template/pkg/models"

// apiResponse is the envelope returned by every Telegram Bot API method.
type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
	Result      T      `json:"result"`
}

// Message is a partial representation of a Telegram message.
type Message struct {
	MessageID int  `json:"message_id"`
	Chat      Chat `json:"chat"`
}

type Chat struct {
	ID int64 `json:"id"`
}

// File holds the Telegram file path for downloading.
type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int    `json:"file_size,omitempty"`
}

// ---- request bodies ----

type sendMessageRequest struct {
	ChatID      int64                        `json:"chat_id"`
	Text        string                       `json:"text"`
	ParseMode   string                       `json:"parse_mode,omitempty"`
	ReplyMarkup *models.InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type sendVoiceRequest struct {
	ChatID int64  `json:"chat_id"`
	Voice  string `json:"voice"` // Telegram file_id
}

type sendPhotoRequest struct {
	ChatID  int64  `json:"chat_id"`
	Photo   string `json:"photo"` // Telegram file_id
	Caption string `json:"caption,omitempty"`
}

type editMessageTextRequest struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int    `json:"message_id"`
	Text      string `json:"text"`
}

type deleteMessageRequest struct {
	ChatID    int64 `json:"chat_id"`
	MessageID int   `json:"message_id"`
}

type getFileRequest struct {
	FileID string `json:"file_id"`
}

type answerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
}
