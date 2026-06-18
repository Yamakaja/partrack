package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChat(t *testing.T) {
	var gotBody chatBody
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"route\":\"generic\"}"}}]}`) //nolint:errcheck
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTPClient: srv.Client()})
	out, err := c.Chat(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONObject: true,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out.Content != `{"route":"generic"}` {
		t.Errorf("content = %q", out.Content)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotBody.Model != "m" || len(gotBody.Messages) != 1 {
		t.Errorf("request body = %+v", gotBody)
	}
	if gotBody.ResponseFormat == nil || gotBody.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format not set: %+v", gotBody.ResponseFormat)
	}
}

func TestChatFallbackModel(t *testing.T) {
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b chatBody
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &b) //nolint:errcheck
		models = append(models, b.Model)
		if b.Model == "primary" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`) //nolint:errcheck
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Model: "primary", FallbackModel: "backup", HTTPClient: srv.Client()})
	out, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out.Content != "ok" {
		t.Errorf("content = %q", out.Content)
	}
	if len(models) != 2 || models[0] != "primary" || models[1] != "backup" {
		t.Errorf("expected primary then backup, got %v", models)
	}
}

func TestChatErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"error":{"message":"bad model"}}`) //nolint:errcheck
	}))
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTPClient: srv.Client()})
	if _, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err == nil {
		t.Fatal("expected error from error body")
	}
}

// captureRawBody starts a server that records the raw request body and returns a
// canned response, plus the client pointed at it.
func captureRawBody(t *testing.T, opts Options) (*string, Client, func()) {
	t.Helper()
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`) //nolint:errcheck
	}))
	opts.BaseURL = srv.URL
	opts.HTTPClient = srv.Client()
	if opts.APIKey == "" {
		opts.APIKey = "k"
	}
	if opts.Model == "" {
		opts.Model = "m"
	}
	return &raw, New(opts), srv.Close
}

func TestReasoningSerialization(t *testing.T) {
	tests := []struct {
		name            string
		effort, style   string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "openai style explicit", effort: "high", style: ReasoningStyleOpenAI,
			wantContains:    []string{`"reasoning_effort":"high"`},
			wantNotContains: []string{`"reasoning":`},
		},
		{
			name: "openrouter style explicit", effort: "high", style: ReasoningStyleOpenRouter,
			wantContains:    []string{`"reasoning":{"effort":"high"}`},
			wantNotContains: []string{`"reasoning_effort"`},
		},
		{
			name: "disabled when effort empty", effort: "", style: ReasoningStyleOpenAI,
			wantNotContains: []string{`"reasoning_effort"`, `"reasoning":`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, c, closeFn := captureRawBody(t, Options{ReasoningEffort: tt.effort, ReasoningStyle: tt.style})
			defer closeFn()
			if _, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(*raw, want) {
					t.Errorf("body %s missing %q", *raw, want)
				}
			}
			for _, no := range tt.wantNotContains {
				if strings.Contains(*raw, no) {
					t.Errorf("body %s should not contain %q", *raw, no)
				}
			}
		})
	}
}

func TestResolveReasoningStyle(t *testing.T) {
	tests := []struct{ style, baseURL, want string }{
		{"", "https://openrouter.ai/api/v1", ReasoningStyleOpenRouter},
		{"", "https://ki-toolbox.scc.kit.edu/api", ReasoningStyleOpenAI},
		{"openrouter", "https://ki-toolbox.scc.kit.edu/api", ReasoningStyleOpenRouter}, // explicit overrides
		{"OpenAI", "https://openrouter.ai/api/v1", ReasoningStyleOpenAI},               // explicit, case-insensitive
		{"bogus", "https://example.com", ReasoningStyleOpenAI},                         // unknown → default
	}
	for _, tt := range tests {
		if got := resolveReasoningStyle(tt.style, tt.baseURL); got != tt.want {
			t.Errorf("resolveReasoningStyle(%q, %q) = %q, want %q", tt.style, tt.baseURL, got, tt.want)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{`{"a":1}`, `{"a":1}`, true},
		{"```json\n{\"a\":1}\n```", `{"a":1}`, true},
		{`sure, here: {"a":{"b":2}} done`, `{"a":{"b":2}}`, true},
		{`{"s":"has } brace"}`, `{"s":"has } brace"}`, true},
		{`no json here`, "", false},
	}
	for _, tt := range tests {
		got, ok := ExtractJSON(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ExtractJSON(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
	if !strings.Contains(`{"x":1}`, mustExtract(t, `prefix {"x":1}`)) {
		t.Error("sanity")
	}
}

func mustExtract(t *testing.T, s string) string {
	t.Helper()
	out, ok := ExtractJSON(s)
	if !ok {
		t.Fatalf("ExtractJSON(%q) failed", s)
	}
	return out
}
