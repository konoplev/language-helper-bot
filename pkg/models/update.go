package models

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

type UpdateType string

const (
	UpdateTypeCommand       UpdateType = "command"
	UpdateTypeText          UpdateType = "text"
	UpdateTypeVoice         UpdateType = "voice"
	UpdateTypeAudio         UpdateType = "audio"
	UpdateTypeImage         UpdateType = "image"
	UpdateTypeCallbackQuery UpdateType = "callback_query"
	UpdateTypeInlineQuery   UpdateType = "inline_query"
	UpdateTypeUnknown       UpdateType = "unknown"
)

type UpdateContext struct {
	Update tgbotapi.Update
	Type   UpdateType
	UserID int64
	ChatID int64
}

func NewUpdateContext(update tgbotapi.Update) UpdateContext {
	uc := UpdateContext{Update: update, Type: UpdateTypeUnknown}

	switch {
	case update.Message != nil:
		if update.Message.From != nil {
			uc.UserID = update.Message.From.ID
		}
		uc.ChatID = update.Message.Chat.ID
		switch {
		case update.Message.IsCommand():
			uc.Type = UpdateTypeCommand
		case update.Message.Voice != nil:
			uc.Type = UpdateTypeVoice
		case update.Message.Audio != nil:
			uc.Type = UpdateTypeAudio
		case update.Message.Photo != nil:
			uc.Type = UpdateTypeImage
		case update.Message.Text != "":
			uc.Type = UpdateTypeText
		}

	case update.CallbackQuery != nil:
		uc.UserID = update.CallbackQuery.From.ID
		if update.CallbackQuery.Message != nil {
			uc.ChatID = update.CallbackQuery.Message.Chat.ID
		}
		uc.Type = UpdateTypeCallbackQuery

	case update.InlineQuery != nil:
		uc.UserID = update.InlineQuery.From.ID
		// Inline queries have no chat; private chat ID equals the user ID.
		uc.ChatID = update.InlineQuery.From.ID
		uc.Type = UpdateTypeInlineQuery
	}

	return uc
}
