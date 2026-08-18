package amz

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MCPProtocolVersion is the revision of the Model Context Protocol this server
// speaks. A client that asks for a different one is answered with this, which is
// what the spec says to do rather than refusing the connection.
const MCPProtocolVersion = "2025-06-18"

// JSON-RPC error codes, from the spec.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// maxRPCLine caps one incoming message.
//
// Requests are small. Responses can be megabytes, and those are written rather
// than read, so this bounds only what a client can push at the server.
const maxRPCLine = 4 << 20

// MCP serves the tool registry over stdio as Model Context Protocol.
//
// One registration, one process, one connection. The transport is newline
// delimited JSON-RPC on stdin and stdout, which is why nothing else in this type
// is allowed to print: a log line on stdout is a protocol error.
type MCP struct {
	Server *Server
	In     io.Reader
	Out    io.Writer
	// Log is where the banner and any diagnostics go. It is stderr in practice
	// and never Out.
	Log io.Writer

	mu sync.Mutex
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Banner is what the server says about itself before the first message.
//
// It goes to stderr, where a human running `amz mcp` by hand will see it and a
// client reading stdout will not.
func (m *MCP) Banner() string {
	return fmt.Sprintf("amz mcp %s, %d read tools, marketplace %s\n%s\n",
		m.Server.Version, len(m.Server.Tools), m.Server.Marketplace, NoRobotsNotice)
}

// Serve reads messages until the input ends or the context is cancelled.
func (m *MCP) Serve(ctx context.Context) error {
	if m.Log != nil {
		_, _ = io.WriteString(m.Log, m.Banner())
	}
	sc := bufio.NewScanner(m.In)
	sc.Buffer(make([]byte, 0, 64<<10), maxRPCLine)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			m.reply(rpcResponse{ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: err.Error()}})
			continue
		}
		// A message with no id is a notification: it gets handled and never
		// answered. Replying to one is the most common way to break a client.
		if len(msg.ID) == 0 {
			continue
		}
		if msg.JSONRPC != "2.0" {
			m.reply(rpcResponse{ID: msg.ID, Error: &rpcError{Code: rpcInvalidRequest,
				Message: `every message must carry "jsonrpc":"2.0"`}})
			continue
		}
		res, rerr := m.handle(ctx, msg)
		m.reply(rpcResponse{ID: msg.ID, Result: res, Error: rerr})
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (m *MCP) handle(ctx context.Context, msg rpcMessage) (any, *rpcError) {
	switch msg.Method {
	case "initialize":
		return m.initialize(), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": m.toolList()}, nil
	case "tools/call":
		return m.call(ctx, msg.Params)
	default:
		return nil, &rpcError{Code: rpcMethodNotFound, Message: "no method " + msg.Method}
	}
}

func (m *MCP) initialize() map[string]any {
	return map[string]any{
		"protocolVersion": MCPProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "amz", "version": m.Server.Version},
		"instructions": "amz reads public Amazon surfaces and returns structured records. " +
			"Every result carries an envelope: the surfaces that were read, the clock they were read at, and `missed`, " +
			"which names what the page did not give up and why. A result with eight reviews and a missed entry saying " +
			"4,812 total is the whole answer, not a truncated one. " +
			"The tools here read only. Crawling, config, the cache and export are not exposed. " + NoRobotsNotice,
	}
}

// toolList renders the registry as MCP tool descriptors.
func (m *MCP) toolList() []map[string]any {
	out := make([]map[string]any, 0, len(m.Server.Tools))
	for _, t := range m.Server.Tools {
		props := map[string]any{}
		var required []string
		for _, a := range t.Args {
			schema := map[string]any{"description": a.Desc}
			if a.Repeated {
				item := map[string]any{"type": a.Type}
				if len(a.Enum) > 0 {
					item["enum"] = a.Enum
				}
				schema["type"] = "array"
				schema["items"] = item
			} else {
				schema["type"] = a.Type
				if len(a.Enum) > 0 {
					schema["enum"] = a.Enum
				}
			}
			props[a.Name] = schema
			if a.Required {
				required = append(required, a.Name)
			}
		}
		schema := map[string]any{
			"type":                 "object",
			"properties":           props,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Summary,
			"inputSchema": schema,
			"annotations": map[string]any{
				"title":           t.Name,
				"readOnlyHint":    true,
				"destructiveHint": false,
				"openWorldHint":   true,
			},
		})
	}
	return out
}

// call runs one tool.
//
// A tool that fails comes back as an ordinary result with isError set, not as a
// JSON-RPC error. That is the protocol's rule and it is the right one here: a
// CAPTCHA or a robots refusal is an answer the model should read and reason
// about, not a transport fault it never sees.
func (m *MCP) call(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: rpcInvalidParams, Message: err.Error()}
		}
	}
	if p.Name == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "tools/call needs a name"}
	}
	if _, ok := m.Server.Lookup(p.Name); !ok {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "no tool named " + p.Name}
	}
	res, err := m.Server.Call(ctx, p.Name, p.Arguments)
	if err != nil {
		code := 1
		var ec exitCoder
		if errors.As(err, &ec) {
			code = ec.ExitCode()
		}
		return mcpText(fmt.Sprintf("%s failed, exit %d: %s", p.Name, code, err), true), nil
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, &rpcError{Code: rpcInternalError, Message: err.Error()}
	}
	return mcpText(string(b), false), nil
}

func mcpText(s string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": s}},
		"isError": isErr,
	}
}

// reply writes one response. It holds a lock because a future revision may
// answer from more than one goroutine, and two interleaved lines on stdout are
// an unrecoverable protocol error rather than a slow response.
func (m *MCP) reply(r rpcResponse) {
	r.JSONRPC = "2.0"
	if len(r.ID) == 0 {
		r.ID = json.RawMessage("null")
	}
	b, err := json.Marshal(r)
	if err != nil {
		b, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: r.ID,
			Error: &rpcError{Code: rpcInternalError, Message: "response could not be encoded"}})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.Out.Write(append(b, '\n'))
	if f, ok := m.Out.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
}
