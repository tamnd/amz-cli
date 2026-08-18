package amz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Tool is one read operation the server exposes.
//
// The registry is built by package cli, which owns the command tree, and handed
// to a Server. That split is the point: amz knows nothing about cobra and cli
// knows nothing about HTTP or JSON-RPC, so the same list of tools is served two
// ways without either transport growing its own idea of what a tool is.
//
// It is also the allowlist. A name that is not in the registry is a 404 and not
// a command that ran, which is how crawl, config, cache clear, open and export
// stay off the wire: they are absent rather than blocked, and there is no string
// a caller can send that reaches them.
type Tool struct {
	Name    string    `json:"name"`
	Summary string    `json:"summary"`
	Args    []ToolArg `json:"args,omitempty"`
}

// ToolArg is one argument of one tool.
//
// Name matches the flag the CLI uses, hyphens and all, so a caller reading
// `amz search --help` already knows what to send.
type ToolArg struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // string, integer, boolean
	Desc     string   `json:"desc,omitempty"`
	Required bool     `json:"required,omitempty"`
	Repeated bool     `json:"repeated,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

// Dispatcher runs one tool and returns what it produced.
type Dispatcher func(ctx context.Context, tool string, args map[string]any) (Records, error)

// Records is the output of one dispatched run.
//
// Rows are the same JSON objects the CLI writes under `-o json`, so a result
// over the wire and a result on a terminal are the same bytes.
//
// Pages carries the envelope of a page whose rows do not each hold one. price,
// offers, reviews, variants, refine and tree are row shaped: a review row has no
// envelope on purpose, because the provenance belongs to the page and repeating
// it on all eight lines would be noise. At a terminal those commands print what
// they missed on stderr. There is no stderr on the wire, so it arrives here.
type Records struct {
	Rows  []json.RawMessage
	Pages []Envelope
}

// NoRobotsNotice is the one sentence the server says about robots.txt.
//
// It appears in the startup banner, in the HTTP index, in the MCP initialize
// instructions and in `amz serve --help`, because a caller that reaches this
// tool through a model has no terminal to read a flag list in and is entitled to
// know what the thing on the other end will and will not do.
const NoRobotsNotice = "--no-robots has no effect here and cannot be set through this server. " +
	"Ignoring robots.txt is a decision a person makes for one run at their own terminal, not something a tool call inherits."

// Server serves a tool registry over HTTP.
type Server struct {
	Tools       []Tool
	Dispatch    Dispatcher
	Marketplace string
	Version     string
}

// Result is what one tool call answers with.
//
// Records carry their own envelopes. Envelope here is the union of them, which
// is what a caller that asked one question wants: the one read it just made, its
// surfaces, its clock, and what it missed.
type Result struct {
	Tool     string            `json:"tool"`
	Params   map[string]any    `json:"params,omitempty"`
	Count    int               `json:"count"`
	Records  []json.RawMessage `json:"records"`
	Envelope ResultEnvelope    `json:"envelope"`
}

// ResultEnvelope is the provenance of a whole tool call.
type ResultEnvelope struct {
	Marketplace string      `json:"marketplace,omitempty"`
	Surfaces    []string    `json:"surfaces,omitempty"`
	Depths      []string    `json:"depths,omitempty"`
	Unread      []string    `json:"unread,omitempty"`
	Robots      *RobotsNote `json:"robots,omitempty"`
	RetrievedAt time.Time   `json:"retrieved_at,omitzero"`

	// Missed is never omitted. An empty list is the answer "the tool looked and
	// there was nothing more", and a missing key would be the answer "this server
	// forgot to say", which are not the same thing and must not serialize the
	// same way. A model calling reviews and getting eight rows needs to be told
	// there are 4,812.
	Missed []Miss `json:"missed"`
}

// ToolError is a tool call that failed, carrying the exit code the CLI would
// have returned for the same run.
type ToolError struct {
	Code int
	Err  error
}

func (e *ToolError) Error() string { return e.Err.Error() }
func (e *ToolError) Unwrap() error { return e.Err }

// ExitCode reports the process exit code this failure maps to.
func (e *ToolError) ExitCode() int { return e.Code }

// exitCoder is implemented by an error that knows its process exit code.
// cli.ExitError implements it too, which is how the status mapping below works
// without amz importing cli.
type exitCoder interface{ ExitCode() int }

// Lookup finds a tool by name.
func (s *Server) Lookup(name string) (Tool, bool) {
	for _, t := range s.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Call runs one tool and assembles the result.
func (s *Server) Call(ctx context.Context, name string, args map[string]any) (*Result, error) {
	if _, ok := s.Lookup(name); !ok {
		return nil, &ToolError{Code: 2, Err: fmt.Errorf("no tool named %q. GET /v1/tools for the list", name)}
	}
	out, err := s.Dispatch(ctx, name, args)
	if err != nil {
		return nil, err
	}
	if out.Rows == nil {
		out.Rows = []json.RawMessage{}
	}
	res := &Result{
		Tool:     name,
		Params:   args,
		Count:    len(out.Rows),
		Records:  out.Rows,
		Envelope: mergeEnvelopes(out.Rows, out.Pages),
	}
	if res.Envelope.Marketplace == "" {
		res.Envelope.Marketplace = s.Marketplace
	}
	return res, nil
}

// mergeEnvelopes folds every record's envelope into one.
//
// Records that carry no envelope, such as a `query` row, contribute nothing and
// are not an error: the caller asked the local store a question and there is no
// page behind the answer.
func mergeEnvelopes(recs []json.RawMessage, pages []Envelope) ResultEnvelope {
	envs := make([]*Envelope, 0, len(recs)+len(pages))
	for _, r := range recs {
		var wrap struct {
			Envelope *Envelope `json:"envelope"`
		}
		if json.Unmarshal(r, &wrap) == nil && wrap.Envelope != nil {
			envs = append(envs, wrap.Envelope)
		}
	}
	for i := range pages {
		envs = append(envs, &pages[i])
	}

	out := ResultEnvelope{Missed: []Miss{}}
	seenSurface := map[string]bool{}
	seenDepth := map[string]bool{}
	seenUnread := map[string]bool{}
	seenMiss := map[string]bool{}
	for _, env := range envs {
		if out.Marketplace == "" {
			out.Marketplace = env.Marketplace
		}
		for _, sf := range env.Surfaces {
			if !seenSurface[sf] {
				seenSurface[sf] = true
				out.Surfaces = append(out.Surfaces, sf)
			}
		}
		if env.Depth != "" && !seenDepth[env.Depth] {
			seenDepth[env.Depth] = true
			out.Depths = append(out.Depths, env.Depth)
		}
		for _, u := range env.Unread {
			if !seenUnread[u] {
				seenUnread[u] = true
				out.Unread = append(out.Unread, u)
			}
		}
		for _, m := range env.Missed {
			k := m.Field + "\x00" + m.Why
			if !seenMiss[k] {
				seenMiss[k] = true
				out.Missed = append(out.Missed, m)
			}
		}
		// The strictest robots note wins. One disallowed read in a batch is the
		// fact worth reporting, not the four that were allowed.
		if env.Robots != nil && (out.Robots == nil || (out.Robots.Allowed && !env.Robots.Allowed)) {
			out.Robots = env.Robots
		}
		if env.RetrievedAt.After(out.RetrievedAt) {
			out.RetrievedAt = env.RetrievedAt
		}
	}
	sort.Strings(out.Surfaces)
	sort.Strings(out.Depths)
	sort.Strings(out.Unread)
	return out
}

// Handler builds the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
	})
	mux.HandleFunc("GET /v1/tools", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tools": s.Tools, "count": len(s.Tools)})
	})
	mux.HandleFunc("GET /v1/tools/{name}", s.run)
	mux.HandleFunc("POST /v1/tools/{name}", s.run)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		names := make([]string, 0, len(s.Tools))
		for _, t := range s.Tools {
			names = append(names, t.Name)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":        "amz",
			"version":     s.Version,
			"marketplace": s.Marketplace,
			"tools":       names,
			"robots":      NoRobotsNotice,
			"endpoints": []string{
				"GET /healthz",
				"GET /v1/tools",
				"GET /v1/tools/{name}?arg=value",
				"POST /v1/tools/{name}",
			},
		})
	})
	return mux
}

// run answers one tool call.
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tool, ok := s.Lookup(name)
	if !ok {
		writeErr(w, http.StatusNotFound, 2, fmt.Errorf("no tool named %q. GET /v1/tools for the list", name))
		return
	}
	args, err := s.args(r, tool)
	if err != nil {
		writeErr(w, http.StatusBadRequest, 2, err)
		return
	}
	res, err := s.Call(r.Context(), name, args)
	if err != nil {
		code := 1
		var ec exitCoder
		if errors.As(err, &ec) {
			code = ec.ExitCode()
		}
		writeErr(w, statusFor(code), code, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// args reads the arguments out of the query string or the JSON body.
//
// A GET carries strings and nothing else, so a value that a tool declares as an
// integer or a boolean is converted here rather than left for the caller to get
// right. A POST carries real JSON and is taken as it comes.
func (s *Server) args(r *http.Request, tool Tool) (map[string]any, error) {
	if r.Method == http.MethodPost && r.ContentLength != 0 {
		var body map[string]any
		dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
		if err := dec.Decode(&body); err != nil {
			return nil, fmt.Errorf("body is not a JSON object: %w", err)
		}
		return body, nil
	}
	q := r.URL.Query()
	args := make(map[string]any, len(q))
	for k, vs := range q {
		spec, known := argSpec(tool, k)
		switch {
		case known && spec.Repeated:
			vals := make([]any, 0, len(vs))
			for _, v := range vs {
				vals = append(vals, v)
			}
			args[k] = vals
		case known && spec.Type == "boolean":
			// `?verified` with no value is how a URL spells a flag, and
			// `?verified=true` is how most clients will send the same thing.
			// Both mean the flag is set.
			v := vs[len(vs)-1]
			args[k] = v == "" || v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		default:
			args[k] = vs[len(vs)-1]
		}
	}
	return args, nil
}

func argSpec(t Tool, name string) (ToolArg, bool) {
	for _, a := range t.Args {
		if a.Name == name {
			return a, true
		}
	}
	return ToolArg{}, false
}

// statusFor maps an amz exit code to an HTTP status.
//
// The codes are the ones documented in `amz why exit-codes` and defined in
// package cli. They are written out here rather than imported because the wire
// mapping belongs to the server, and because amz does not depend on cli.
func statusFor(code int) int {
	// 3 (no data) and 4 (partial) are not in this table on purpose. A tool call
	// that read the page and found nothing succeeded, and the dispatcher turns
	// both into an ordinary result whose envelope says what was missing. They
	// only reach here if something has gone wrong upstream of that.
	switch code {
	case 0:
		return http.StatusOK
	case 2: // usage
		return http.StatusBadRequest
	case 5, 7, 9: // captcha, robots disallowed, sign-in wall
		return http.StatusForbidden
	case 6: // interstitial, worth retrying later
		return http.StatusTooManyRequests
	case 8: // robots.txt could not be fetched
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status, code int, err error) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"exit":    code,
			"message": strings.TrimSpace(err.Error()),
		},
	})
}
