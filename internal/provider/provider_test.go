package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

func TestStreamOpenAITextAndUsage(t *testing.T) {
	t.Setenv("TEST_KEY", "k")
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "include_usage") {
			t.Error("expected stream_options include_usage")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := newHTTPClient
	newHTTPClient = func() *http.Client { return srv.Client() }
	t.Cleanup(func() { newHTTPClient = old })

	ch, err := StreamWithUsage(context.Background(), config.Model{
		Provider: "openai", Model: "gpt", CredentialEnv: "TEST_KEY", BaseURL: srv.URL,
	}, []Message{{Role: "user", Content: "q"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var text, usage bool
	for ev := range ch {
		if ev.Type == "text" && ev.Text == "hi" {
			text = true
		}
		if ev.Type == "usage" && ev.Usage != nil && ev.Usage.InputTokens == 3 && ev.Usage.OutputTokens == 2 {
			usage = true
		}
	}
	if !text || !usage {
		t.Fatalf("text=%v usage=%v", text, usage)
	}
}

func TestStreamOpenAIErrorStatus(t *testing.T) {
	t.Setenv("TEST_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	old := newHTTPClient
	newHTTPClient = func() *http.Client { return srv.Client() }
	t.Cleanup(func() { newHTTPClient = old })
	_, err := Stream(context.Background(), config.Model{
		Provider: "openai", Model: "gpt", CredentialEnv: "TEST_KEY", BaseURL: srv.URL,
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("got %v", err)
	}
}

func TestStreamMissingKey(t *testing.T) {
	os.Unsetenv("MISSING_ABOX_KEY")
	_, err := Stream(context.Background(), config.Model{CredentialEnv: "MISSING_ABOX_KEY"}, nil, nil)
	if err == nil {
		t.Fatal("expected missing credential")
	}
}

func TestStreamAnthropicText(t *testing.T) {
	t.Setenv("TEST_KEY", "k")
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`data: {"type":"content_block_delta","delta":{"text":"yo"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		"",
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := newHTTPClient
	newHTTPClient = func() *http.Client { return srv.Client() }
	t.Cleanup(func() { newHTTPClient = old })
	ch, err := Stream(context.Background(), config.Model{
		Provider: "anthropic", Model: "claude", CredentialEnv: "TEST_KEY", BaseURL: srv.URL,
	}, []Message{{Role: "user", Content: "q"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var text, usage bool
	for ev := range ch {
		if ev.Type == "text" && ev.Text == "yo" {
			text = true
		}
		if ev.Type == "usage" && ev.Usage != nil && ev.Usage.InputTokens == 4 && ev.StopReason == "end_turn" {
			usage = true
		}
	}
	if !text || !usage {
		t.Fatalf("text=%v usage=%v", text, usage)
	}
}
