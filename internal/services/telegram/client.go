package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"deutsch-helper/pkg/models"
)

const DefaultBaseURL = "https://api.telegram.org"

type Client struct {
	token      string
	baseURL    string
	maxRetries int
	retryDelay time.Duration
	http       *http.Client
	logger     *slog.Logger
}

type Option func(*Client)

func WithBaseURL(u string) Option           { return func(c *Client) { c.baseURL = u } }
func WithMaxRetries(n int) Option           { return func(c *Client) { c.maxRetries = n } }
func WithRetryDelay(d time.Duration) Option { return func(c *Client) { c.retryDelay = d } }
func WithHTTPClient(h *http.Client) Option  { return func(c *Client) { c.http = h } }

func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		token:      token,
		baseURL:    DefaultBaseURL,
		maxRetries: 3,
		retryDelay: 300 * time.Millisecond,
		http:       &http.Client{Timeout: 30 * time.Second},
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) apiURL(method string) string {
	return fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)
}

func (c *Client) fileURL(filePath string) string {
	return fmt.Sprintf("%s/file/bot%s/%s", c.baseURL, c.token, filePath)
}

// SendMessage sends a text message and returns the new message ID.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (int, error) {
	msg, err := c.sendMessage(ctx, &sendMessageRequest{ChatID: chatID, Text: text})
	if err != nil {
		return 0, err
	}
	return msg.MessageID, nil
}

// SendMessageWithKeyboard sends a text message with an inline keyboard.
func (c *Client) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, kb models.InlineKeyboardMarkup) (int, error) {
	msg, err := c.sendMessage(ctx, &sendMessageRequest{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: &kb,
	})
	if err != nil {
		return 0, err
	}
	return msg.MessageID, nil
}

func (c *Client) sendMessage(ctx context.Context, req *sendMessageRequest) (*Message, error) {
	var result Message
	if err := c.postJSON(ctx, "sendMessage", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendVoice sends a voice message by Telegram file_id.
func (c *Client) SendVoice(ctx context.Context, chatID int64, fileID string) (int, error) {
	var result Message
	if err := c.postJSON(ctx, "sendVoice", &sendVoiceRequest{ChatID: chatID, Voice: fileID}, &result); err != nil {
		return 0, err
	}
	return result.MessageID, nil
}

// SendPhoto sends a photo by Telegram file_id with an optional caption.
func (c *Client) SendPhoto(ctx context.Context, chatID int64, fileID string, caption string) (int, error) {
	var result Message
	req := &sendPhotoRequest{ChatID: chatID, Photo: fileID, Caption: caption}
	if err := c.postJSON(ctx, "sendPhoto", req, &result); err != nil {
		return 0, err
	}
	return result.MessageID, nil
}

// EditMessageText edits the text of an existing message.
func (c *Client) EditMessageText(ctx context.Context, chatID int64, messageID int, text string) error {
	req := &editMessageTextRequest{ChatID: chatID, MessageID: messageID, Text: text}
	var result Message
	return c.postJSON(ctx, "editMessageText", req, &result)
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	req := &deleteMessageRequest{ChatID: chatID, MessageID: messageID}
	var result bool
	return c.postJSON(ctx, "deleteMessage", req, &result)
}

// GetFile returns the file path for a given file ID.
func (c *Client) GetFile(ctx context.Context, fileID string) (string, error) {
	var result File
	if err := c.postJSON(ctx, "getFile", &getFileRequest{FileID: fileID}, &result); err != nil {
		return "", err
	}
	return result.FilePath, nil
}

// DownloadFile downloads a file by its Telegram file path.
func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.fileURL(filePath), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// AnswerCallbackQuery acknowledges a callback query.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID string) error {
	var result bool
	return c.postJSON(ctx, "answerCallbackQuery", &answerCallbackQueryRequest{CallbackQueryID: callbackID}, &result)
}

// SetMyCommands registers the bot's command list with Telegram so they appear in the menu.
func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	var result bool
	return c.postJSON(ctx, "setMyCommands", &setMyCommandsRequest{Commands: commands}, &result)
}

// postJSON marshals body, POSTs to the given API method, and unmarshals the result field.
func (c *Client) postJSON(ctx context.Context, method string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(method), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var envelope apiResponse[json.RawMessage]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram error: %s", envelope.Description)
	}
	if result != nil {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

// doWithRetry executes req with exponential backoff on 5xx / rate-limit responses.
// The caller is responsible for setting the request body before calling; for retries
// the request must be idempotent (GET) or use a fixed body snapshot via postJSON.
func (c *Client) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Snapshot the body so we can replay it on retries.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			c.logger.WarnContext(ctx, "telegram request failed", slog.Int("attempt", attempt+1), slog.String("error", err.Error()))
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			c.logger.WarnContext(ctx, "telegram retryable error", slog.Int("attempt", attempt+1), slog.Int("status", resp.StatusCode))
			continue
		}

		return resp, nil
	}
	return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
}
