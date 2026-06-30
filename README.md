# deutsch-helper

A Telegram bot for language learning. It helps you translate text between your native and target language and gives detailed grammar feedback on your writing.

---

## Features

- **User allowlist** — only Telegram users listed in `allowed_users.txt` can interact with the bot.
- **Personalised setup** — `/start` guides you through choosing your native language, the language you're learning, and your CEFR proficiency level. Settings are saved persistently and only need to be set once.
- **Three commands, always available** in the Telegram command menu:
  - `/from` — translate text from your native language into the language you're learning, at your level.
  - `/to` — translate text from the language you're learning into your native language.
  - `/polish` — find grammar and style mistakes, explain each one (in your native language), and propose a corrected version.
- **Voice input** — for any active command, you can send a voice message instead of typing:
  1. The bot transcribes the audio and sends the transcription back.
  2. You confirm or edit it by sending your final text.
  3. The command processes your final text.
- The active command persists until you select another one.

---

## Getting started

### Prerequisites

- Go 1.22 or later
- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- A [Groq](https://console.groq.com) API key (for voice transcription)
- An [OpenAI](https://platform.openai.com) API key (for translation and grammar checks)

### 1 — Configure allowed users

Edit `allowed_users.txt` and add the Telegram user ID of each person who may use the bot, one per line. To find your own ID, message [@userinfobot](https://t.me/userinfobot).

```
# allowed_users.txt
123456789
987654321
```

### 2 — Set environment variables

```bash
export BOT_TOKEN="123456789:AAHdqTcvCH1vGWJxfSeofSs3yqD1IVpITG4"
export GROQ_API_KEY="gsk_..."
export OPENAI_API_KEY="sk-..."
export OPENAI_MODEL="gpt-4o"          # optional, default: gpt-4o
export ALLOWED_USERS_FILE="allowed_users.txt"  # optional, this is the default
export PREFS_FILE="data/prefs.json"   # optional, this is the default
export LOG_LEVEL="info"               # optional: info or debug
export POLLING_MODE="long_polling"    # optional: long_polling or webhook
```

### 3 — Run

```bash
go run ./cmd/bot
```

---

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `BOT_TOKEN` | yes | — | Telegram bot token |
| `GROQ_API_KEY` | yes | — | Groq API key (voice transcription via Whisper) |
| `OPENAI_API_KEY` | yes | — | OpenAI API key |
| `OPENAI_MODEL` | no | `gpt-4o` | OpenAI model |
| `ALLOWED_USERS_FILE` | no | `allowed_users.txt` | Path to allowlist file |
| `PREFS_FILE` | no | `data/prefs.json` | Path for persistent user settings |
| `LOG_LEVEL` | no | `info` | `debug` or `info` |
| `POLLING_MODE` | no | `long_polling` | `long_polling` or `webhook` |

---

## Usage

### First run — setup

Send `/start` to the bot. It will ask you:

1. **Your native language** — pick from an inline keyboard.
2. **The language you're learning** — pick from an inline keyboard.
3. **Your current level** — A1 through C2.

Your settings are saved to disk and survive bot restarts. Run `/start` again at any time to update them.

### Translating

- **`/from`** — activates "translate from native" mode. Send any text (or voice) in your native language and receive a translation at your level.
- **`/to`** — activates "translate to native" mode. Send text in your target language and receive a translation to your native language.

The active mode persists until you choose another command.

### Grammar and style check

**`/polish`** — activates the correction mode. Send text in the language you're learning. The bot replies with:

```
Original:
[your text]

Mistakes:
1. [mistake]
2. [mistake]

Explanations:
1. [explanation in your native language, including the grammar rule]
2. ...

Improved version:
[corrected text close to your original]
```

### Voice input

Any active command accepts voice messages:

1. Record and send a voice note.
2. The bot transcribes it and sends the transcription back.
3. Send the text back (edit it first if needed).
4. The command processes your text.

---

## Project structure

```
deutsch-helper/
├── cmd/bot/main.go                  Entry point
├── internal/
│   ├── bot/
│   │   ├── bot.go                   Bot struct, wiring, Run()
│   │   ├── dispatcher.go            Routes updates to handlers
│   │   └── webhook.go               HTTP handler for webhook mode
│   ├── flows/
│   │   ├── engine.go                Flow state machine engine
│   │   ├── setup_flow.go            Setup flow constants and keyboards
│   │   ├── voice_flow.go            Voice flow constants
│   │   └── ...                      Storage, state types
│   ├── handlers/
│   │   ├── handler.go               Handler, TelegramAPI, PrefsStore, TextProcessor interfaces
│   │   ├── command.go               /start /from /to /polish
│   │   ├── voice.go                 Voice transcription flow
│   │   ├── text.go                  Text processing + AI dispatch
│   │   └── callback.go              Setup flow button presses
│   ├── middleware/
│   │   ├── allowlist.go             User allowlist gate
│   │   └── middleware.go            Recovery, Timing, Logging
│   ├── services/
│   │   ├── groq/client.go           Groq Whisper transcription client
│   │   ├── openai/client.go         OpenAI Responses API client
│   │   └── telegram/client.go       Telegram HTTP client
│   └── store/
│       ├── prefs.go                 In-memory PrefsStore (for tests)
│       └── file_prefs.go            JSON file-backed persistent PrefsStore
└── pkg/models/
    ├── config.go                    Config loaded from environment
    ├── settings.go                  UserSettings, Language helpers
    ├── update.go                    UpdateContext wrapping tgbotapi updates
    └── keyboard.go                  Inline keyboard helpers
```

---

## Testing

```bash
go test ./...
```

Tests use real HTTP clients pointed at local `httptest.Server` instances — no mocking frameworks.

| Package | Coverage |
|---|---|
| `internal/services/openai` | Success, API error, context cancellation, auth header, instructions field |
| `internal/services/groq` | Transcription, retry on 5xx, auth, context cancellation |
| `internal/services/telegram` | All methods, retry, error responses |
| `internal/middleware` | Allowlist allow/deny/zero-ID, file parsing, bad lines |
| `internal/store` | Get/save, persistence across reload, copy isolation, concurrency, nested dirs |
| `internal/flows` | Engine get/set/clear, IsInFlow, IsInState, storage isolation |
| `internal/handlers` | Dispatch routing, command routing for all commands |
| `internal/integration` | Voice flow, text→AI, setup flow, command activation, retry, rejection without setup |

---

## Docker

```bash
docker build -t deutsch-helper .
docker run \
  -e BOT_TOKEN="..." \
  -e GROQ_API_KEY="..." \
  -e OPENAI_API_KEY="..." \
  -v $(pwd)/allowed_users.txt:/app/allowed_users.txt:ro \
  -v $(pwd)/data:/app/data \
  deutsch-helper
```
