package mcp

import (
	"bufio"
	"bytes"
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
	ID     any       `json:"id"`
	Method string    `json:"method"`
	Params rpcParams `json:"params"`
}

// rpcParams carries the fields this server reads from the JSON-RPC params
// object. `arguments` stays free-format on purpose: the MCP protocol defines
// it per tool through the published schema (documented free-format seam).
type rpcParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// rpcReply is the JSON-RPC success envelope; rpcFailure is its error
// counterpart. Two shapes because the wire carries exactly one of `result`
// or `error` — an omitempty union would drop ping's empty result (map 棘轮).
type rpcReply struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result"`
}

type rpcFailure struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      any          `json:"id"`
	Error   rpcErrorBody `json:"error"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve runs the newline-delimited JSON-RPC loop until the input ends or the
// context is cancelled.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) (err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	writer := bufio.NewWriter(w)
	// A flush failure means a response died on the way out; surface it instead
	// of dropping it (the stdio channel is the client's only reply path).
	defer func() {
		if ferr := writer.Flush(); ferr != nil && err == nil {
			err = ferr
		}
	}()
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
	var info struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(payload, &info); err != nil {
		return "", err
	}
	return info.Label, nil
}

// --- MCP method results ----------------------------------------------------

// initResult answers initialize. The tools capability object is empty today
// (no listChanged), same as the map it replaced.
type initResult struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    initCapabilities `json:"capabilities"`
	ServerInfo      serverInfo       `json:"serverInfo"`
}

type initCapabilities struct {
	Tools struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"tools"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// toolListResult answers tools/list.
type toolListResult struct {
	Tools []toolSpec `json:"tools"`
}

// toolSpec is one published tool. InputSchema's properties table is keyed by
// argument name — the JSON-Schema shape, one of the protocol's documented
// free-format seams (the property nodes themselves are typed).
type toolSpec struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema toolSchema `json:"inputSchema"`
}

type toolSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]toolSchemaProp `json:"properties"`
	Required   []string                 `json:"required,omitempty"`
}

type toolSchemaProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// toolCallResult answers tools/call: one text block plus the error flag.
type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) handle(ctx context.Context, msg rpcMessage) any {
	switch msg.Method {
	case "initialize":
		return reply(msg.ID, initResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo:      serverInfo{Name: "open-voice-bridge", Version: "dev"},
		})
	case "ping":
		return reply(msg.ID, struct{}{})
	case "tools/list":
		return reply(msg.ID, toolListResult{Tools: toolSpecs()})
	case "tools/call":
		return s.callTool(ctx, msg)
	default:
		return rpcFailure{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   rpcErrorBody{Code: -32601, Message: "method not found: " + msg.Method},
		}
	}
}

func reply(id any, result any) rpcReply {
	return rpcReply{JSONRPC: "2.0", ID: id, Result: result}
}

func toolSpecs() []toolSpec {
	return []toolSpec{
		{
			Name:        "history_list",
			Description: "List recent dictation history (local only; the text was spoken by the user and typed into their apps). Newest first.",
			InputSchema: toolSchema{
				Type: "object",
				Properties: map[string]toolSchemaProp{
					"limit": {Type: "number", Description: "max entries, default 20, capped at 200"},
				},
			},
		},
		{
			Name:        "history_search",
			Description: "Search dictation history by substring (case-insensitive). Newest first.",
			InputSchema: toolSchema{
				Type: "object",
				Properties: map[string]toolSchemaProp{
					"query": {Type: "string", Description: "substring to find"},
					"limit": {Type: "number", Description: "max entries, default 20, capped at 200"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "usage_stats",
			Description: "Cumulative dictation statistics: utterance count, characters, speech time, longest single dictation and per-day totals. Counts only, never content.",
			InputSchema: toolSchema{Type: "object", Properties: map[string]toolSchemaProp{}},
		},
	}
}

func (s *Server) callTool(ctx context.Context, msg rpcMessage) any {
	name := msg.Params.Name
	args := msg.Params.Arguments
	var payload json.RawMessage
	var err error
	switch name {
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
		return toolError(msg.ID, "unknown tool: "+name)
	}
	if err != nil {
		return toolError(msg.ID, err.Error())
	}
	var text bytes.Buffer
	if indentErr := json.Indent(&text, payload, "", "  "); indentErr != nil {
		return toolError(msg.ID, indentErr.Error())
	}
	return reply(msg.ID, toolCallResult{
		Content: []toolContent{{Type: "text", Text: text.String()}},
		IsError: false,
	})
}

func limitArg(args map[string]any) string {
	if value, ok := args["limit"].(float64); ok && value > 0 {
		return strconv.Itoa(int(value))
	}
	return "20"
}

func toolError(id any, message string) any {
	return reply(id, toolCallResult{
		Content: []toolContent{{Type: "text", Text: message}},
		IsError: true,
	})
}

// fetch calls one read-only agent endpoint with the client token. A 401 means
// the grant was revoked while this session was open (撤销即失效). The body
// comes back raw: the tool result re-serves it verbatim (no decode/re-encode
// round trip), and Verify decodes just the field it needs.
func (s *Server) fetch(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
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
	defer func() { _ = resp.Body.Close() }()
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
	if !json.Valid(body) {
		return nil, fmt.Errorf("agent endpoint returned invalid JSON")
	}
	return json.RawMessage(body), nil
}
