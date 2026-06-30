package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const DefaultBaseURL = "https://api.openai.com"

type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
	logger  *slog.Logger
}

type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = u } }
func WithModel(m string) Option            { return func(c *Client) { c.model = m } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func NewClient(apiKey, model string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
		logger:  slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type responsesRequest struct {
	Model        string `json:"model"`
	Instructions string `json:"instructions,omitempty"`
	Input        string `json:"input"`
}

type responsesResponse struct {
	Output []outputItem `json:"output"`
	Error  *apiError    `json:"error,omitempty"`
}

type outputItem struct {
	Type    string         `json:"type"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Complete sends systemPrompt + userText to the OpenAI Responses API and returns
// the assistant's reply as plain text.
func (c *Client) Complete(ctx context.Context, systemPrompt, userText string) (string, error) {
	reqBody := responsesRequest{
		Model:        c.model,
		Instructions: systemPrompt,
		Input:        userText,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/responses", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	c.logger.DebugContext(ctx, "openai request",
		slog.String("model", c.model),
		slog.Int("prompt_len", len(systemPrompt)),
		slog.Int("input_len", len(userText)),
	)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	c.logger.DebugContext(ctx, "openai response",
		slog.Int("status", resp.StatusCode),
	)

	if resp.StatusCode != http.StatusOK {
		var errResp responsesResponse
		if json.Unmarshal(raw, &errResp) == nil && errResp.Error != nil {
			return "", fmt.Errorf("openai error: %s", errResp.Error.Message)
		}
		return "", fmt.Errorf("openai returned %d: %s", resp.StatusCode, raw)
	}

	var result responsesResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	// Extract text from the first output_text content block.
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		for _, block := range item.Content {
			if block.Type == "output_text" && block.Text != "" {
				return block.Text, nil
			}
		}
	}

	return "", fmt.Errorf("no text content in openai response")
}
