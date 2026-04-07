package chat

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestNewAnthropicClient_DefaultModel(t *testing.T) {
	c := NewAnthropicClient("test-key", "")
	if c.model != "claude-sonnet-4-6" {
		t.Errorf("expected default model claude-sonnet-4-6, got %s", c.model)
	}
	if c.client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewAnthropicClient_CustomModel(t *testing.T) {
	c := NewAnthropicClient("test-key", "claude-haiku-4-5-20251001")
	if c.model != "claude-haiku-4-5-20251001" {
		t.Errorf("expected model claude-haiku-4-5-20251001, got %s", c.model)
	}
}

func TestConvertMessages_UserAndAssistant(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
		{Role: "user", Content: "How are you?"},
	}
	params := convertMessages(msgs)

	if len(params) != 3 {
		t.Fatalf("expected 3 params, got %d", len(params))
	}
	if params[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("expected user role, got %s", params[0].Role)
	}
	if params[1].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("expected assistant role, got %s", params[1].Role)
	}
	if params[2].Role != anthropic.MessageParamRoleUser {
		t.Errorf("expected user role, got %s", params[2].Role)
	}
}

func TestConvertMessages_SkipsUnknownRoles(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "system", Content: "You are helpful"},
		{Role: "unknown", Content: "ignored"},
	}
	params := convertMessages(msgs)

	if len(params) != 1 {
		t.Fatalf("expected 1 param (only user), got %d", len(params))
	}
}

func TestConvertMessages_Empty(t *testing.T) {
	params := convertMessages(nil)
	if len(params) != 0 {
		t.Fatalf("expected 0 params, got %d", len(params))
	}
}

func TestExtractText_SingleBlock(t *testing.T) {
	msg := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Hello world"},
		},
	}
	got := extractText(msg)
	if got != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", got)
	}
}

func TestExtractText_MultipleBlocks(t *testing.T) {
	msg := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Hello "},
			{Type: "text", Text: "world"},
		},
	}
	got := extractText(msg)
	if got != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", got)
	}
}

func TestExtractText_SkipsNonTextBlocks(t *testing.T) {
	msg := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Hello"},
			{Type: "tool_use", Text: ""},
			{Type: "text", Text: " world"},
		},
	}
	got := extractText(msg)
	if got != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", got)
	}
}

func TestExtractText_EmptyContent(t *testing.T) {
	msg := &anthropic.Message{}
	got := extractText(msg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
