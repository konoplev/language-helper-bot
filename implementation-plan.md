# Implementation Plan: Language Learning Bot

## Overview

Transform the `go-telegram-template` into a language-learning Telegram bot that helps users translate text and improve their writing in a foreign language.

## Goals

1. User allowlist via `allowed_users.txt`
2. `/start` — guided setup: native language, learning language, CEFR level
3. Three persistent commands: `/from`, `/to`, `/polish`
4. Voice flow: transcribe → confirm/edit → process
5. OpenAI Responses API for translation and grammar correction
6. Register bot commands in Telegram menu

---

## Architecture Changes

### Module rename
`go-telegram-template` → `deutsch-helper`

### New packages
- `internal/services/openai` — OpenAI Responses API client
- `internal/middleware/allowlist.go` — user allowlist middleware
- `internal/store/file_prefs.go` — JSON-file-based persistent preferences
- `internal/flows/setup_flow.go` — setup flow constants

### Modified packages
- `pkg/models/config.go` — add `OPENAI_API_KEY`, `OPENAI_MODEL`, `ALLOWED_USERS_FILE`, `PREFS_FILE`
- `pkg/models/settings.go` (new) — `UserSettings` struct, `Language` type, helpers
- `internal/handlers/handler.go` — redesign `PrefsStore` and add `TextProcessor` interfaces
- `internal/handlers/command.go` — new commands: `/start`, `/from`, `/to`, `/polish`
- `internal/handlers/voice.go` — simplified voice flow (transcribe → send back → await text)
- `internal/handlers/text.go` — process text with active command via OpenAI
- `internal/handlers/callback.go` — handle setup flow button presses
- `internal/flows/voice_flow.go` — add `StateVoicePending`, `PayloadActiveCommand`
- `internal/store/prefs.go` — redesigned `PrefsStore` interface + `InMemoryPrefs`
- `internal/services/telegram/client.go` — add `SetMyCommands`
- `internal/bot/bot.go` — wire new handlers and call `SetMyCommands` on startup

### Removed
- `internal/handlers/audio.go` — not in spec
- `internal/handlers/image.go` — not in spec
- `internal/flows/language_flow.go` — replaced by `setup_flow.go`
- Old voice "Edit / Send as text / Send as command" callback buttons

---

## Data Model

### UserSettings (pkg/models/settings.go)
```
NativeLanguage   string   // ISO 639-1, e.g. "en"
LearningLanguage string   // ISO 639-1, e.g. "de"
Level            string   // CEFR: A1/A2/B1/B2/C1/C2
ActiveCommand    string   // "from" | "to" | "polish" | ""
```

### PrefsStore interface (handlers/handler.go)
```
GetSettings(ctx, userID) (*models.UserSettings, bool)
SaveSettings(ctx, userID, *models.UserSettings) error
```

### TextProcessor interface (handlers/handler.go)
```
Complete(ctx, systemPrompt, userText string) (string, error)
```

---

## User Flows

### Setup Flow (/start)
```
/start
  → send native language keyboard  [state: setup/native]
User picks native language
  → save to flow payload
  → send learning language keyboard  [state: setup/learning]
User picks learning language
  → save to flow payload
  → send level keyboard  [state: setup/level]
User picks level
  → save UserSettings to PrefsStore
  → clear flow state
  → send confirmation with command list
```

### Command Activation
```
/from  → settings.ActiveCommand = "from" → "Ready, send text in [native]"
/to    → settings.ActiveCommand = "to"   → "Ready, send text in [learning]"
/polish → settings.ActiveCommand = "polish" → "Ready, send text in [learning]"
```

### Voice Flow (per active command)
```
User sends voice
  → if not configured: ask to /start
  → if no active command: ask to select command
  → determine transcription lang (native for /from, learning for /to//polish)
  → transcribe (Groq)
  → send transcription back
  → set state: voice/pending  [payload: transcription, active_command]

User sends text (any text)
  → if state voice/pending: use that text, clear state
  → if active command set: process with command
  → if not configured/no command: guidance message
```

### Text Flow
```
User sends text
  → if in setup flow: "Please use the buttons"
  → if in voice/pending state: take as final text, process with stored command
  → if not configured: "Please run /start"
  → if no active command: "Please select a command: /from /to /polish"
  → otherwise: process text with active command
```

---

## OpenAI Prompts

### /from (translate native → learning at level)
```
System: Translate the following text from [native] to [learning].
The translation must be at CEFR level [level].
Respond with ONLY the translation, no explanations or extra text.
```

### /to (translate learning → native)
```
System: Translate the following text from [learning] to [native].
Respond with ONLY the translation, no explanations or extra text.
```

### /polish (correct + explain)
```
System: You are a language teacher. The student is learning [learning] at CEFR level [level].
Their native language is [native].

Analyze the student's text written in [learning] and provide a response with these sections:

Original:
[copy the original text]

Mistakes:
[numbered list of mistakes found]

Explanations:
[for each mistake: explanation in [native], the grammar rule violated, examples if helpful]

Improved version:
[corrected text, as close as possible to the original style]
```

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `BOT_TOKEN` | yes | — | Telegram bot token |
| `GROQ_API_KEY` | yes | — | Groq API key (voice transcription) |
| `OPENAI_API_KEY` | yes | — | OpenAI API key |
| `OPENAI_MODEL` | no | `gpt-4o` | OpenAI model |
| `ALLOWED_USERS_FILE` | no | `allowed_users.txt` | Path to allowlist |
| `PREFS_FILE` | no | `data/prefs.json` | Path for persistent user settings |
| `LOG_LEVEL` | no | `info` | `debug` or `info` |
| `POLLING_MODE` | no | `long_polling` | `long_polling` or `webhook` |

---

## Test Coverage

- `internal/services/openai` — HTTP-level tests: success, API error, context cancel
- `internal/middleware` — allowlist: allowed, denied, zero userID, file parsing
- `internal/store` — file prefs: get/save, persistence across reload, concurrent access
- `internal/flows` — engine tests remain; setup/voice flow constants tested indirectly
- `internal/handlers` — dispatch routing, command routing for new commands
- `internal/integration` — end-to-end: setup flow, command activation, voice→text→AI, direct text→AI

---

## Steps

- [x] Write this plan
- [x] Rename module go-telegram-template → deutsch-helper
- [x] Add UserSettings and language helpers to pkg/models
- [x] Update Config to include new env vars
- [x] Create OpenAI client + tests
- [x] Create allowlist middleware + tests
- [x] Create setup_flow.go constants
- [x] Redesign store/prefs.go + file_prefs.go + tests
- [x] Update handler interfaces (handler.go)
- [x] Rewrite command handler
- [x] Rewrite voice handler
- [x] Rewrite text handler
- [x] Rewrite callback handler
- [x] Add SetMyCommands to telegram client
- [x] Rewire bot.go
- [x] Delete removed files (audio.go, image.go, language_flow.go)
- [x] Update integration tests
- [x] Update dispatch_test.go
- [x] Update README.md
- [x] Create allowed_users.txt
- [x] Update docker-compose.yml
