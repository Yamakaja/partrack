// Package llm is a minimal OpenAI-compatible chat-completions client used for the
// optional LLM fallback that interprets BOM line items the deterministic parser
// can't read cleanly. It defaults to OpenRouter so small open-weight models
// (Llama/Gemma) can be used. It is transport-only: prompt construction, JSON
// schema validation, and repair/degrade logic live in the caller.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Chat message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a single chat-completion request.
type Request struct {
	Messages []Message
	// JSONObject asks the provider to constrain output to a JSON object via
	// response_format. Best-effort — many open models ignore it, so callers must
	// still parse defensively (see ExtractJSON).
	JSONObject bool
	// Temperature; use 0 for deterministic extraction.
	Temperature float64
}

// Client is a chat-completions client. Implementations are safe for concurrent use.
type Client interface {
	Chat(ctx context.Context, req Request) (string, error)
}

// Reasoning styles select how a reasoning-effort request is serialized, because
// providers disagree on the wire format (see Options.ReasoningStyle).
const (
	ReasoningStyleOpenAI     = "openai"     // top-level "reasoning_effort"
	ReasoningStyleOpenRouter = "openrouter" // "reasoning": {"effort": ...} object
)

// Options configures an OpenRouter (OpenAI-compatible) client.
type Options struct {
	BaseURL       string
	APIKey        string
	Model         string
	FallbackModel string // tried once if Model errors; optional
	Timeout       time.Duration
	HTTPClient    *http.Client // optional; injected by tests
	Debug         bool         // log every request/response to stderr
	// ReasoningEffort, when non-empty, asks a reasoning-capable model to think
	// before answering ("minimal"/"low"/"medium"/"high"; OpenRouter also accepts
	// "none"/"xhigh"). Empty disables reasoning.
	ReasoningEffort string
	// ReasoningStyle is "openai" or "openrouter"; empty auto-detects from BaseURL
	// (openrouter.ai → openrouter, else openai). Ignored when ReasoningEffort is "".
	ReasoningStyle string
}

type openRouter struct {
	http            *http.Client
	baseURL         string
	apiKey          string
	model           string
	fallbackModel   string
	debug           bool
	reasoningEffort string
	reasoningStyle  string
}

// resolveReasoningStyle normalizes an explicit style or auto-detects from baseURL.
func resolveReasoningStyle(style, baseURL string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case ReasoningStyleOpenAI:
		return ReasoningStyleOpenAI
	case ReasoningStyleOpenRouter:
		return ReasoningStyleOpenRouter
	}
	if strings.Contains(strings.ToLower(baseURL), "openrouter.ai") {
		return ReasoningStyleOpenRouter
	}
	return ReasoningStyleOpenAI
}

// New builds an OpenRouter client. BaseURL and Timeout fall back to defaults.
func New(opts Options) Client {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	hc := opts.HTTPClient
	if hc == nil {
		to := opts.Timeout
		if to <= 0 {
			to = 30 * time.Second
		}
		hc = &http.Client{Timeout: to}
	}
	return &openRouter{
		http:            hc,
		baseURL:         base,
		apiKey:          opts.APIKey,
		model:           opts.Model,
		fallbackModel:   opts.FallbackModel,
		debug:           opts.Debug,
		reasoningEffort: strings.TrimSpace(opts.ReasoningEffort),
		reasoningStyle:  resolveReasoningStyle(opts.ReasoningStyle, base),
	}
}

// Chat sends a completion request, retrying once with the fallback model if the
// primary errors.
func (c *openRouter) Chat(ctx context.Context, req Request) (string, error) {
	content, err := c.chatModel(ctx, c.model, req)
	if err != nil && c.fallbackModel != "" && c.fallbackModel != c.model {
		return c.chatModel(ctx, c.fallbackModel, req)
	}
	return content, err
}

type chatBody struct {
	Model          string       `json:"model"`
	Messages       []Message    `json:"messages"`
	Temperature    float64      `json:"temperature"`
	ResponseFormat *responseFmt `json:"response_format,omitempty"`
	// Reasoning: exactly one shape is set, per the configured style.
	ReasoningEffort string         `json:"reasoning_effort,omitempty"` // openai style
	Reasoning       *reasoningSpec `json:"reasoning,omitempty"`        // openrouter style
}

type reasoningSpec struct {
	Effort string `json:"effort"`
}

type responseFmt struct {
	Type string `json:"type"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content          string          `json:"content"`
			Reasoning        string          `json:"reasoning"`
			ReasoningDetails json.RawMessage `json:"reasoning_details"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *openRouter) chatModel(ctx context.Context, model string, req Request) (string, error) {
	body := chatBody{Model: model, Messages: req.Messages, Temperature: req.Temperature}
	if req.JSONObject {
		body.ResponseFormat = &responseFmt{Type: "json_object"}
	}
	if c.reasoningEffort != "" {
		switch c.reasoningStyle {
		case ReasoningStyleOpenRouter:
			body.Reasoning = &reasoningSpec{Effort: c.reasoningEffort}
		default: // ReasoningStyleOpenAI
			body.ReasoningEffort = c.reasoningEffort
		}
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	if c.debug {
		reasoning := "off"
		if c.reasoningEffort != "" {
			reasoning = fmt.Sprintf("%s/%s", c.reasoningStyle, c.reasoningEffort)
		}
		log.Printf("llm [request] model=%s json_object=%t reasoning=%s", model, req.JSONObject, reasoning)
		for _, m := range req.Messages {
			log.Printf("llm [request] %s:\n%s", m.Role, m.Content)
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("X-Title", "partrack")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if c.debug {
		log.Printf("llm [response] model=%s status=%d body:\n%s", model, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("llm: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var cr chatResp
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("llm: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm: empty response")
	}
	if c.debug && c.reasoningEffort != "" {
		tokens := 0
		if cr.Usage != nil && cr.Usage.CompletionTokensDetails != nil {
			tokens = cr.Usage.CompletionTokensDetails.ReasoningTokens
		}
		log.Printf("llm [reasoning] tokens=%d trace:\n%s", tokens, strings.TrimSpace(cr.Choices[0].Message.Reasoning))
	}
	return cr.Choices[0].Message.Content, nil
}

// ExtractJSON returns the first balanced JSON object embedded in s, or ok=false.
// Small models routinely wrap JSON in prose or ```json fences; this digs it out
// so the caller can unmarshal it. String contents (incl. escaped quotes/braces)
// are skipped when tracking brace depth.
func ExtractJSON(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}
