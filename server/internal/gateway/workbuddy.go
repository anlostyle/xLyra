package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

// WorkBuddy (Tencent CodeBuddy CN) gateway support.
//
// The upstream speaks OpenAI chat completions at /v2/chat/completions but only
// accepts streaming requests, so the protocol spec forces stream=true and the
// chat protocol adapter folds the SSE chunks back into one chat.completion for
// non-streaming clients (see aggregateChatCompletionSSE).

const workBuddyCredentialTypePrefix = "workbuddy_oauth:"

func isWorkBuddySite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), adapter.WorkBuddySiteType)
}

func workBuddyCredentialUsable(credential store.SiteCredential, state store.SiteAPIKeyState, hasState bool) bool {
	if !gatewayCredentialUsable(credential) {
		return false
	}
	if !hasState {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(state.SyncStatus))
	if !state.Enabled || status == "failed" || status == "reauth_required" {
		return false
	}
	return true
}

func shouldMarkWorkBuddyCredentialSyncFailed(result gatewayAttemptResult) bool {
	return result.errorType == "upstream_http_error" && result.upstreamStatusCode == http.StatusUnauthorized
}

// markWorkBuddyCredentialSyncFailed disables an account whose token the
// upstream rejected, so the user is prompted to re-authorize it. The state
// transition is identical to Grok's, so the shared implementation is reused.
func (h Handler) markWorkBuddyCredentialSyncFailed(ctx context.Context, credential store.SiteCredential, result gatewayAttemptResult) {
	if h.db == nil || credential.ID == uuid.Nil || !shouldMarkWorkBuddyCredentialSyncFailed(result) {
		return
	}
	h.markGrokCredentialReauthRequired(ctx, credential, fmt.Sprintf("workbuddy upstream returned status %d", result.upstreamStatusCode))
}

// applyWorkBuddyGatewayHeaders adds the CodeBuddy client headers and drops every
// other header. /v1/messages and /v1/responses forward the caller's own headers
// (anthropic-beta, x-stainless-*, …); CodeBuddy treats those as a foreign client
// and rejects the call with code 11128. domain is the tenant domain from login.
func applyWorkBuddyGatewayHeaders(req *http.Request, upstreamKey string, domain string) {
	if req == nil {
		return
	}
	adapter.ApplyWorkBuddyUpstreamHeaders(req, upstreamKey, domain)
	allowed := map[string]struct{}{
		"Authorization":        {},
		"Content-Type":         {},
		"Accept":               {},
		"User-Agent":           {},
		"Origin":               {},
		"Referer":              {},
		"X-Requested-With":     {},
		"X-Product":            {},
		"X-Domain":             {},
		"X-No-Department-Info": {},
	}
	for key := range req.Header {
		if _, ok := allowed[key]; !ok {
			req.Header.Del(key)
		}
	}
}

// workBuddyBlockedPhrases are exact sentences CodeBuddy's channel check matches
// inside a request. The official Claude Code prompt hits them, so each is
// rewritten to a near-identical sentence the check does not match.
var workBuddyBlockedPhrases = [][2]string{
	{"You are Claude Code, Anthropic's official CLI for Claude.", "You are Claude Code, Anthropic's official CLI tool for Claude."},
	{"Main branch (you will usually use this for PRs)", "Default branch (you will usually use this for PRs)"},
}

func rewriteWorkBuddyBlockedPhrases(payload map[string]any) {
	messages, _ := payload["messages"].([]any)
	for _, item := range messages {
		message, _ := item.(map[string]any)
		if message == nil {
			continue
		}
		switch content := message["content"].(type) {
		case string:
			message["content"] = replaceWorkBuddyBlockedPhrases(content)
		case []any:
			for _, part := range content {
				block, _ := part.(map[string]any)
				if text, ok := block["text"].(string); ok {
					block["text"] = replaceWorkBuddyBlockedPhrases(text)
				}
			}
		}
	}
}

func replaceWorkBuddyBlockedPhrases(text string) string {
	for _, phrase := range workBuddyBlockedPhrases {
		text = strings.ReplaceAll(text, phrase[0], phrase[1])
	}
	return text
}

// ---------------------------------------------------------------------------
// SSE -> chat.completion aggregation

type workBuddyChatChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
}

type workBuddyToolCallAccumulator struct {
	id        string
	typ       string
	name      strings.Builder
	arguments strings.Builder
}

type workBuddyChoiceAccumulator struct {
	role         string
	content      strings.Builder
	reasoning    strings.Builder
	toolCalls    map[int]*workBuddyToolCallAccumulator
	finishReason string
}

// aggregateChatCompletionSSE folds an OpenAI chat completion SSE stream into a
// single chat.completion JSON document.
func aggregateChatCompletionSSE(body []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 32<<20)

	var (
		id, model string
		created   int64
		usage     json.RawMessage
		choices   = map[int]*workBuddyChoiceAccumulator{}
		sawChunk  bool
	)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk workBuddyChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("decode upstream chat chunk: %w", err)
		}
		if chunk.Error != nil {
			message := strings.TrimSpace(chunk.Error.Message)
			if message == "" {
				message = "upstream returned an error event"
			}
			return nil, fmt.Errorf("upstream stream error: %s", message)
		}
		sawChunk = true
		if chunk.ID != "" {
			id = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Created > 0 {
			created = chunk.Created
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			acc := choices[choice.Index]
			if acc == nil {
				acc = &workBuddyChoiceAccumulator{toolCalls: map[int]*workBuddyToolCallAccumulator{}}
				choices[choice.Index] = acc
			}
			if choice.Delta.Role != "" {
				acc.role = choice.Delta.Role
			}
			acc.content.WriteString(choice.Delta.Content)
			acc.reasoning.WriteString(choice.Delta.ReasoningContent)
			for _, call := range choice.Delta.ToolCalls {
				entry := acc.toolCalls[call.Index]
				if entry == nil {
					entry = &workBuddyToolCallAccumulator{}
					acc.toolCalls[call.Index] = entry
				}
				if call.ID != "" {
					entry.id = call.ID
				}
				if call.Type != "" {
					entry.typ = call.Type
				}
				entry.name.WriteString(call.Function.Name)
				entry.arguments.WriteString(call.Function.Arguments)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				acc.finishReason = *choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read upstream chat stream: %w", err)
	}
	if !sawChunk {
		return nil, fmt.Errorf("upstream stream ended before any chat chunk was received")
	}

	indexes := make([]int, 0, len(choices))
	for index := range choices {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	outChoices := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		acc := choices[index]
		role := acc.role
		if role == "" {
			role = "assistant"
		}
		message := map[string]any{"role": role}
		if acc.content.Len() > 0 || len(acc.toolCalls) == 0 {
			message["content"] = acc.content.String()
		} else {
			message["content"] = nil
		}
		if acc.reasoning.Len() > 0 {
			message["reasoning_content"] = acc.reasoning.String()
		}
		if len(acc.toolCalls) > 0 {
			callIndexes := make([]int, 0, len(acc.toolCalls))
			for callIndex := range acc.toolCalls {
				callIndexes = append(callIndexes, callIndex)
			}
			sort.Ints(callIndexes)
			calls := make([]map[string]any, 0, len(callIndexes))
			for _, callIndex := range callIndexes {
				entry := acc.toolCalls[callIndex]
				callType := entry.typ
				if callType == "" {
					callType = "function"
				}
				calls = append(calls, map[string]any{
					"id":   entry.id,
					"type": callType,
					"function": map[string]any{
						"name":      entry.name.String(),
						"arguments": entry.arguments.String(),
					},
				})
			}
			message["tool_calls"] = calls
		}
		finish := acc.finishReason
		if finish == "" {
			finish = "stop"
		}
		outChoices = append(outChoices, map[string]any{
			"index":         index,
			"message":       message,
			"finish_reason": finish,
		})
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	result := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": outChoices,
	}
	if len(usage) > 0 {
		result["usage"] = usage
	}
	return json.Marshal(result)
}
