package flows

import "go-telegram-template/pkg/models"

const (
	FlowLanguage        FlowName  = "language"
	StateLanguageSelect StateName = "select"

	// PayloadPendingVoiceID holds the Telegram file_id of a voice message that
	// arrived before the user had chosen a language; processed automatically once
	// the language is selected.
	PayloadPendingVoiceID = "pending_voice_id"

	CallbackLangPrefix = "lang:" // e.g. lang:en, lang:ru
)

// Language pairs an ISO-639-1 code with a display label.
type Language struct {
	Code  string
	Label string
}

// SupportedLanguages is the list shown to users in the language-selection keyboard.
var SupportedLanguages = []Language{
	{"en", "🇬🇧 English"},
	{"ru", "🇷🇺 Русский"},
	{"de", "🇩🇪 Deutsch"},
	{"fr", "🇫🇷 Français"},
	{"es", "🇪🇸 Español"},
	{"it", "🇮🇹 Italiano"},
	{"pt", "🇵🇹 Português"},
	{"zh", "🇨🇳 中文"},
	{"ja", "🇯🇵 日本語"},
	{"ar", "🇸🇦 العربية"},
}

// LanguageKeyboard builds an inline keyboard with two languages per row.
func LanguageKeyboard() models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(SupportedLanguages); i += 2 {
		row := []models.InlineKeyboardButton{
			models.NewCallbackButton(SupportedLanguages[i].Label, CallbackLangPrefix+SupportedLanguages[i].Code),
		}
		if i+1 < len(SupportedLanguages) {
			row = append(row, models.NewCallbackButton(SupportedLanguages[i+1].Label, CallbackLangPrefix+SupportedLanguages[i+1].Code))
		}
		rows = append(rows, row)
	}
	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// LanguageLabel returns the display label for a language code, or the code itself if unknown.
func LanguageLabel(code string) string {
	for _, l := range SupportedLanguages {
		if l.Code == code {
			return l.Label
		}
	}
	return code
}
