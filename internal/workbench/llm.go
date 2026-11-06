package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Protocol-neutral conversation model. Each protocol adapter converts to/from its wire format.
type Turn struct {
	Role   string     // user | assistant | tool
	Text   string     // user/assistant text, or JSON tool result
	Calls  []ToolCall // assistant tool calls
	CallID string     // tool result: id of the call it answers
	Name   string     // tool result: tool name (Gemini matches by name)
}
type ToolCall struct {
	ID   string
	Name string
	Args json.RawMessage
}
type ToolDef struct {
	Name        string
	Description string
	Params      map[string]any // JSON schema object; nil for no-arg tools
}
type Reply struct {
	Text  string
	Calls []ToolCall
	Stop  string // end_turn | tool_use | max_tokens
}

var protocols = map[string]bool{"openai": true, "anthropic": true, "gemini": true}

func modelReady(m ModelProfile) bool {
	return m.BaseURL != "" && m.Model != "" && (m.APIKeyRef != "" || m.APIKeyEnv == "" || os.Getenv(m.APIKeyEnv) != "")
}

// complete runs one model call. final=true keeps tool definitions in context but forbids further calls.
func complete(ctx context.Context, m ModelProfile, key, system string, turns []Turn, tools []ToolDef, final bool) (Reply, error) {
	if m.BaseURL == "" || m.Model == "" {
		return Reply{}, errors.New("请先在偏好设置中添加模型，并为 Agent 选择模型")
	}
	base := strings.TrimRight(m.BaseURL, "/")
	switch m.Protocol {
	case "anthropic":
		return anthropicCall(ctx, base, key, m.Model, system, turns, tools, final)
	case "gemini":
		return geminiCall(ctx, base, key, m.Model, system, turns, tools, final)
	default:
		return openaiCall(ctx, base, key, m.Model, system, turns, tools, final)
	}
}

func postJSON(ctx context.Context, url, key string, headers map[string]string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, e := httpClient.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	data, e := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if e != nil {
		return e
	}
	if res.StatusCode != 200 {
		detail := string(data)
		if key != "" {
			detail = strings.ReplaceAll(detail, key, "[redacted]")
		}
		return fmt.Errorf("模型服务返回 HTTP %d：%s", res.StatusCode, clip(detail, 300))
	}
	return json.Unmarshal(data, out)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
func validArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage("{}")
	}
	return raw
}
func schemaOr(p map[string]any) map[string]any {
	if p == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return p
}

// --- OpenAI Chat Completions (also DeepSeek, Qwen, Ollama, vLLM, OpenRouter …) ---

func openaiCall(ctx context.Context, base, key, model, system string, turns []Turn, tools []ToolDef, final bool) (Reply, error) {
	msgs := []map[string]any{{"role": "system", "content": system}}
	for _, t := range turns {
		switch t.Role {
		case "assistant":
			m := map[string]any{"role": "assistant", "content": t.Text}
			if len(t.Calls) > 0 {
				calls := []map[string]any{}
				for _, c := range t.Calls {
					calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Args)}})
				}
				m["tool_calls"] = calls
				if t.Text == "" {
					m["content"] = nil
				}
			}
			msgs = append(msgs, m)
		case "tool":
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": t.CallID, "content": t.Text})
		default:
			msgs = append(msgs, map[string]any{"role": "user", "content": t.Text})
		}
	}
	body := map[string]any{"model": model, "messages": msgs}
	if len(tools) > 0 {
		defs := []map[string]any{}
		for _, d := range tools {
			fn := map[string]any{"name": d.Name, "description": d.Description}
			if d.Params != nil {
				fn["parameters"] = d.Params
			}
			defs = append(defs, map[string]any{"type": "function", "function": fn})
		}
		body["tools"] = defs
		if final {
			body["tool_choice"] = "none"
		}
	}
	headers := map[string]string{}
	if key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	var res struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if e := postJSON(ctx, base+"/chat/completions", key, headers, body, &res); e != nil {
		return Reply{}, e
	}
	if len(res.Choices) == 0 {
		return Reply{}, errors.New("模型未返回内容")
	}
	ch := res.Choices[0]
	r := Reply{Text: ch.Message.Content, Stop: "end_turn"}
	for _, c := range ch.Message.ToolCalls {
		r.Calls = append(r.Calls, ToolCall{ID: c.ID, Name: c.Function.Name, Args: validArgs(json.RawMessage(c.Function.Arguments))})
	}
	if len(r.Calls) > 0 {
		r.Stop = "tool_use"
	} else if ch.FinishReason == "length" {
		r.Stop = "max_tokens"
	}
	return r, nil
}

// --- Anthropic Messages ---

func anthropicCall(ctx context.Context, base, key, model, system string, turns []Turn, tools []ToolDef, final bool) (Reply, error) {
	msgs := []map[string]any{}
	for _, t := range turns {
		switch t.Role {
		case "assistant":
			content := []map[string]any{}
			if t.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": t.Text})
			}
			for _, c := range t.Calls {
				content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Args})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": content})
		case "tool": // all results of one assistant turn go into a single user message
			block := map[string]any{"type": "tool_result", "tool_use_id": t.CallID, "content": t.Text}
			if n := len(msgs); n > 0 && msgs[n-1]["role"] == "user" {
				if blocks, ok := msgs[n-1]["content"].([]map[string]any); ok {
					msgs[n-1]["content"] = append(blocks, block)
					continue
				}
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": []map[string]any{block}})
		default:
			msgs = append(msgs, map[string]any{"role": "user", "content": t.Text})
		}
	}
	body := map[string]any{"model": model, "system": system, "max_tokens": 8192, "messages": msgs}
	if len(tools) > 0 { // tool_use/tool_result blocks in history require tools to stay defined
		defs := []map[string]any{}
		for _, d := range tools {
			defs = append(defs, map[string]any{"name": d.Name, "description": d.Description, "input_schema": schemaOr(d.Params)})
		}
		body["tools"] = defs
		if final {
			body["tool_choice"] = map[string]any{"type": "none"}
		}
	}
	var res struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if e := postJSON(ctx, base+"/messages", key, map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, body, &res); e != nil {
		return Reply{}, e
	}
	r := Reply{Stop: "end_turn"}
	for _, c := range res.Content {
		switch c.Type {
		case "text":
			r.Text += c.Text
		case "tool_use":
			r.Calls = append(r.Calls, ToolCall{ID: c.ID, Name: c.Name, Args: validArgs(c.Input)})
		}
	}
	if len(r.Calls) > 0 {
		r.Stop = "tool_use"
	} else if res.StopReason == "max_tokens" {
		r.Stop = "max_tokens"
	}
	if r.Text == "" && len(r.Calls) == 0 {
		return Reply{}, fmt.Errorf("模型未返回内容（stop_reason=%s）", res.StopReason)
	}
	return r, nil
}

// --- Google Gemini generateContent ---

func geminiCall(ctx context.Context, base, key, model, system string, turns []Turn, tools []ToolDef, final bool) (Reply, error) {
	contents := []map[string]any{}
	for _, t := range turns {
		switch t.Role {
		case "assistant":
			parts := []map[string]any{}
			if t.Text != "" {
				parts = append(parts, map[string]any{"text": t.Text})
			}
			for _, c := range t.Calls {
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": c.Name, "args": c.Args}})
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case "tool":
			var response any = map[string]any{"result": json.RawMessage(t.Text)}
			if !json.Valid([]byte(t.Text)) {
				response = map[string]any{"result": t.Text}
			}
			part := map[string]any{"functionResponse": map[string]any{"name": t.Name, "response": response}}
			if n := len(contents); n > 0 && contents[n-1]["role"] == "user" {
				if parts, ok := contents[n-1]["parts"].([]map[string]any); ok && len(parts) > 0 && parts[0]["functionResponse"] != nil {
					contents[n-1]["parts"] = append(parts, part)
					continue
				}
			}
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{part}})
		default:
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": t.Text}}})
		}
	}
	body := map[string]any{"system_instruction": map[string]any{"parts": []map[string]any{{"text": system}}}, "contents": contents}
	if len(tools) > 0 {
		defs := []map[string]any{}
		for _, d := range tools {
			fn := map[string]any{"name": d.Name, "description": d.Description}
			if d.Params != nil {
				// Use JSON Schema for open MCP argument objects as well.
				fn["parametersJsonSchema"] = d.Params
			}
			defs = append(defs, fn)
		}
		body["tools"] = []map[string]any{{"functionDeclarations": defs}}
		if final {
			body["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}
		}
	}
	var res struct {
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if e := postJSON(ctx, base+"/models/"+model+":generateContent", key, map[string]string{"x-goog-api-key": key}, body, &res); e != nil {
		return Reply{}, e
	}
	if len(res.Candidates) == 0 {
		return Reply{}, errors.New("模型未返回内容")
	}
	cand := res.Candidates[0]
	r := Reply{Stop: "end_turn"}
	for _, p := range cand.Content.Parts {
		if p.FunctionCall != nil {
			r.Calls = append(r.Calls, ToolCall{ID: id("call"), Name: p.FunctionCall.Name, Args: validArgs(p.FunctionCall.Args)})
		} else {
			r.Text += p.Text
		}
	}
	if len(r.Calls) > 0 {
		r.Stop = "tool_use"
	} else if cand.FinishReason == "MAX_TOKENS" {
		r.Stop = "max_tokens"
	}
	return r, nil
}
