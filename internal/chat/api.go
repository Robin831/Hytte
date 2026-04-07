package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient wraps the Anthropic SDK for multi-turn chat.
type AnthropicClient struct {
	client *anthropic.Client
	model  anthropic.Model
}

// NewAnthropicClient creates a new client for the Anthropic Messages API.
// The apiKey is used for authentication; model specifies which Claude model to use.
func NewAnthropicClient(apiKey string, model string) *AnthropicClient {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return &AnthropicClient{
		client: &client,
		model:  anthropic.Model(model),
	}
}

// ChatMessage represents a single message in a multi-turn conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SendMessage sends a multi-turn conversation to the Anthropic Messages API
// and returns the assistant's text response.
func (c *AnthropicClient) SendMessage(ctx context.Context, messages []ChatMessage) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 4096,
		Messages:  convertMessages(messages),
	}

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic API error: %w", err)
	}

	return extractText(resp), nil
}

// StreamCallback is called for each text chunk during streaming.
type StreamCallback func(text string)

// SendMessageStream sends a multi-turn conversation to the Anthropic Messages API
// with streaming enabled. The callback is called for each text chunk as it arrives.
// Returns the complete accumulated response text.
func (c *AnthropicClient) SendMessageStream(ctx context.Context, messages []ChatMessage, callback StreamCallback) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 4096,
		Messages:  convertMessages(messages),
	}

	stream := c.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var full strings.Builder
	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_delta" {
			delta := event.Delta
			if delta.Type == "text_delta" {
				full.WriteString(delta.Text)
				if callback != nil {
					callback(delta.Text)
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("anthropic stream error: %w", err)
	}

	return full.String(), nil
}

// convertMessages converts ChatMessage slices to the SDK's message format.
func convertMessages(messages []ChatMessage) []anthropic.MessageParam {
	params := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "user":
			params = append(params, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case "assistant":
			params = append(params, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}
	return params
}

// extractText concatenates all text blocks from an API response.
func extractText(resp *anthropic.Message) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}
