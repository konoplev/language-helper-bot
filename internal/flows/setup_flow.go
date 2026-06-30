package flows

import "deutsch-helper/pkg/models"

const (
	FlowSetup          FlowName  = "setup"
	StateSetupNative   StateName = "native"
	StateSetupLearning StateName = "learning"
	StateSetupLevel    StateName = "level"

	PayloadSetupNative   = "setup_native"
	PayloadSetupLearning = "setup_learning"

	CallbackSetupNativePrefix   = "sn:"
	CallbackSetupLearningPrefix = "sl:"
	CallbackSetupLevelPrefix    = "sv:"
)

// NativeLanguageKeyboard builds the inline keyboard for picking the native language.
func NativeLanguageKeyboard() models.InlineKeyboardMarkup {
	return languageKeyboard(CallbackSetupNativePrefix)
}

// LearningLanguageKeyboard builds the inline keyboard for picking the language to learn.
func LearningLanguageKeyboard() models.InlineKeyboardMarkup {
	return languageKeyboard(CallbackSetupLearningPrefix)
}

func languageKeyboard(prefix string) models.InlineKeyboardMarkup {
	langs := models.SupportedLanguages
	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(langs); i += 2 {
		row := []models.InlineKeyboardButton{
			models.NewCallbackButton(langs[i].Name, prefix+langs[i].Code),
		}
		if i+1 < len(langs) {
			row = append(row, models.NewCallbackButton(langs[i+1].Name, prefix+langs[i+1].Code))
		}
		rows = append(rows, row)
	}
	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// LevelKeyboard builds the inline keyboard for picking a CEFR level.
func LevelKeyboard() models.InlineKeyboardMarkup {
	levels := models.CEFRLevels
	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(levels); i += 3 {
		var row []models.InlineKeyboardButton
		for j := i; j < i+3 && j < len(levels); j++ {
			row = append(row, models.NewCallbackButton(levels[j], CallbackSetupLevelPrefix+levels[j]))
		}
		rows = append(rows, row)
	}
	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}
