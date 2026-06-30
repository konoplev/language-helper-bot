package models

// UserSettings stores per-user language learning configuration.
type UserSettings struct {
	NativeLanguage   string // ISO 639-1 code, e.g. "en"
	LearningLanguage string // ISO 639-1 code, e.g. "de"
	Level            string // CEFR level: A1, A2, B1, B2, C1, C2
	ActiveCommand    string // "from", "to", "polish", or ""
}

// IsConfigured returns true when all required setup fields are filled.
func (s *UserSettings) IsConfigured() bool {
	return s.NativeLanguage != "" && s.LearningLanguage != "" && s.Level != ""
}

// Language pairs an ISO 639-1 code with a human-readable name.
type Language struct {
	Code string
	Name string
}

// SupportedLanguages is the full list available during setup.
var SupportedLanguages = []Language{
	{"en", "English"},
	{"ru", "Russian"},
	{"de", "German"},
	{"fr", "French"},
	{"es", "Spanish"},
	{"it", "Italian"},
	{"pt", "Portuguese"},
	{"zh", "Chinese"},
	{"ja", "Japanese"},
	{"ar", "Arabic"},
	{"uk", "Ukrainian"},
	{"nl", "Dutch"},
	{"pl", "Polish"},
	{"tr", "Turkish"},
	{"sv", "Swedish"},
	{"ko", "Korean"},
}

// CEFRLevels lists the supported proficiency levels in order.
var CEFRLevels = []string{"A1", "A2", "B1", "B2", "C1", "C2"}

// LanguageName returns the display name for a language code, or the code itself.
func LanguageName(code string) string {
	for _, l := range SupportedLanguages {
		if l.Code == code {
			return l.Name
		}
	}
	return code
}
