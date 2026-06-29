package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

const (
	DefaultBaseURL    = "https://api.groq.com"
	DefaultModel      = "whisper-large-v3-turbo"
	DefaultMaxRetries = 3
	DefaultLanguage   = "en"
)

type Client struct {
	apiKey     string
	baseURL    string
	model      string
	language   string
	maxRetries int
	retryDelay time.Duration
	http       *http.Client
	logger     *slog.Logger
}

type Option func(*Client)

func WithBaseURL(u string) Option           { return func(c *Client) { c.baseURL = u } }
func WithModel(m string) Option             { return func(c *Client) { c.model = m } }
func WithLanguage(l string) Option          { return func(c *Client) { c.language = l } }
func WithMaxRetries(n int) Option           { return func(c *Client) { c.maxRetries = n } }
func WithRetryDelay(d time.Duration) Option { return func(c *Client) { c.retryDelay = d } }
func WithHTTPClient(h *http.Client) Option  { return func(c *Client) { c.http = h } }

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		model:      DefaultModel,
		language:   DefaultLanguage,
		maxRetries: DefaultMaxRetries,
		retryDelay: 500 * time.Millisecond,
		http:       &http.Client{Timeout: 60 * time.Second},
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type transcriptionResponse struct {
	Text string `json:"text"`
}

// Transcribe sends audioData to Groq and returns the recognised text.
// language is an ISO-639-1 code; if empty, the client-level default is used.
func (c *Client) Transcribe(ctx context.Context, audioData []byte, filename string, language string) (string, error) {
	lang := language
	if lang == "" {
		lang = c.language
	}
	var (
		resp *transcriptionResponse
		err  error
	)
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err = c.doTranscribe(ctx, audioData, filename, lang)
		if err == nil {
			return resp.Text, nil
		}
		c.logger.WarnContext(ctx, "transcription attempt failed",
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)
	}
	return "", fmt.Errorf("transcription failed after %d attempts: %w", c.maxRetries+1, err)
}

func (c *Client) doTranscribe(ctx context.Context, audioData []byte, filename string, lang string) (*transcriptionResponse, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	// Set Content-Type: audio/ogg explicitly so Groq identifies the codec correctly.
	// CreateFormFile uses application/octet-stream which can confuse the API.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", "audio/ogg")
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(audioData); err != nil {
		return nil, err
	}

	if err = w.WriteField("model", c.model); err != nil {
		return nil, err
	}
	if err = w.WriteField("response_format", "json"); err != nil {
		return nil, err
	}
	if lang != "" {
		if err = w.WriteField("language", lang); err != nil {
			return nil, err
		}
	}
	w.Close()

	endpoint := c.baseURL + "/openai/v1/audio/transcriptions"

	c.logger.DebugContext(ctx, "groq transcription request",
		slog.String("endpoint", endpoint),
		slog.String("model", c.model),
		slog.String("language", lang),
		slog.String("filename", filename),
		slog.Int("audio_bytes", len(audioData)),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	c.logger.DebugContext(ctx, "groq transcription response",
		slog.Int("status", httpResp.StatusCode),
		slog.String("body", string(raw)),
	)

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("groq returned %d: %s", httpResp.StatusCode, raw)
	}

	var result transcriptionResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
