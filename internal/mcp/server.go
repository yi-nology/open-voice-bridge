package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Server adapts one AI client's stdio MCP session to the read-only agent
// endpoints. It holds a single client token; every tool call carries it as a
// bearer, so a revoke from the admin UI takes effect on the next call.
type Server struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewServer(baseURL, token string, client *http.Client) *Server {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Server{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: client}
}

type rpcMessage struct {
	ID     any            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// Serve runs the newline-delimited JSON-RPC loop until the input ends or the
// context is cancelled.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	writer := bufio.NewWriter(w)
	defer writer.Flush()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.ID == nil {
			continue // notification: no response
		}
		response := s.handle(ctx, msg)
		data, err := json.Marshal(response)
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// Verify checks the token against the agent endpoint so a bad grant fails at
// startup instead of mid-session. It returns the grant's label.
func (s *Server) Verify(ctx context.Context) (string, error) {
	payload, err := s.fetch(ctx, "/api/agent/verify", nil)
	if err != nil {
		return "", err
	}
	label, _ := payload["label"].(string)
	return label, nil
}

func (s *Server) handle(ctx context.Context, msg rpcMessage) map[string]any {
	switch msg.Method {
	case "initialize":
		return result(msg.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "open-voice-bridge", "version": "dev"},
		})
	case "ping":
		return result(msg.ID, map[string]any{})
	case "tools/list":
		return result(msg.ID, map[string]any{"tools": toolSpecs()})
	case "tools/call":
		return s.callTool(ctx, msg)
	default:
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"error":   map[string]any{"code": -32601, "message": "method not found: " + msg.Method},
		}
	}
}

func result(id any, value map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": value}
}

func toolSpecs() []map[string]any {
	return []map[string]any{
		{
			"name":        "history_list",
			"description": "List recent dictation history (local only; the text was spoken by the user and typed into their apps). Newest first.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"limit": map[string]any{"type": "number", "description": "max entries, default 20, capped at 200"}},
			},
		},
		{
			"name":        "history_search",
			"description": "Search dictation history by substring (case-insensitive). Newest first.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "substring to find"},
					"limit": map[string]any{"type": "number", "description": "max entries, default 20, capped at 200"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "usage_stats",
			"description": "Cumulative dictation statistics: utterance count, characters, speech time, longest single dictation and per-day totals. Counts only, never content.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func (s *Server) callTool(ctx context.Context, msg rpcMessage) map[string]any {
	params, _ := msg.Params["name"].(string)
	args, _ := msg.Params["arguments"].(map[string]any)
	var payload map[string]any
	var err error
	switch params {
	case "history_list":
		payload, err = s.fetch(ctx, "/api/agent/history", url.Values{
			"limit": {limitArg(args)},
		})
	case "history_search":
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return toolError(msg.ID, "history_search requires a query")
		}
		payload, err = s.fetch(ctx, "/api/agent/history", url.Values{
			"limit": {limitArg(args)},
			"q":     {query},
		})
	case "usage_stats":
		payload, err = s.fetch(ctx, "/api/agent/stats", nil)
	default:
		return toolError(msg.ID, "unknown tool: "+params)
	}
	if err != nil {
		return toolError(msg.ID, err.Error())
	}
	text, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return toolError(msg.ID, marshalErr.Error())
	}
	return result(msg.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": false,
	})
}

func limitArg(args map[string]any) string {
	if value, ok := args["limit"].(float64); ok && value > 0 {
		return strconv.Itoa(int(value))
	}
	return "20"
}

func toolError(id any, message string) map[string]any {
	return result(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": message}},
		"isError": true,
	})
}

// fetch calls one read-only agent endpoint with the client token. A 401 means
// the grant was revoked while this session was open (撤销即失效).
func (s *Server) fetch(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	endpoint := s.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("token rejected (%d): access was revoked or is unknown", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent endpoint returned %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}
