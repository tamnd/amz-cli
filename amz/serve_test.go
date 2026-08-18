package amz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stub builds a server whose dispatcher answers with whatever is handed to it.
//
// The point of the split between this package and cli is that the transport can
// be tested without a command tree behind it, so these tests never build one.
func stub(t *testing.T, recs Records, err error) *Server {
	t.Helper()
	return &Server{
		Marketplace: "us",
		Version:     "test",
		Tools: []Tool{
			{Name: "reviews", Summary: "reviews", Args: []ToolArg{
				{Name: "asin", Type: "string", Required: true},
				{Name: "verified", Type: "boolean"},
				{Name: "refine", Type: "string", Repeated: true},
			}},
		},
		Dispatch: func(_ context.Context, _ string, _ map[string]any) (Records, error) {
			return recs, err
		},
	}
}

func parseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func rec(t *testing.T, env Envelope) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{"asin": "B0", "envelope": env})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestMissedIsNeverOmitted: an absent key and an empty list must not serialize
// the same way. Empty means the tool looked and there was nothing more. Absent
// would mean the server forgot to say, and a consumer cannot tell those apart
// after the fact.
func TestMissedIsNeverOmitted(t *testing.T) {
	res, err := stub(t, Records{}, nil).Call(t.Context(), "reviews", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"missed":[]`) {
		t.Fatalf("missed was omitted:\n%s", b)
	}
	if !strings.Contains(string(b), `"records":[]`) {
		t.Fatalf("an empty result serialized records as null:\n%s", b)
	}
	if res.Envelope.Marketplace != "us" {
		t.Errorf("the server marketplace did not fill in an empty envelope: %q", res.Envelope.Marketplace)
	}
}

// TestMergeUnionsAndDedupes: two records read off the same page must not report
// the same surface or the same miss twice.
func TestMergeUnionsAndDedupes(t *testing.T) {
	miss := Miss{Field: "reviews", Why: "behind a sign-in", Have: 2, Total: 284512}
	one := Envelope{Marketplace: "us", Surfaces: []string{"s1"}, Depth: "meta",
		Missed: []Miss{miss}, Unread: []string{"imageBlock"}, RetrievedAt: parseTime(t, "2026-08-18T10:00:00Z")}
	two := Envelope{Marketplace: "us", Surfaces: []string{"s1", "s3"}, Depth: "full",
		Missed:      []Miss{miss, {Field: "questions", Why: "no ask region"}},
		RetrievedAt: parseTime(t, "2026-08-18T11:00:00Z")}

	got := mergeEnvelopes([]json.RawMessage{rec(t, one), rec(t, two)}, nil)
	if want := []string{"s1", "s3"}; strings.Join(got.Surfaces, ",") != strings.Join(want, ",") {
		t.Errorf("surfaces: want %v, got %v", want, got.Surfaces)
	}
	if want := []string{"full", "meta"}; strings.Join(got.Depths, ",") != strings.Join(want, ",") {
		t.Errorf("depths: want %v, got %v", want, got.Depths)
	}
	if len(got.Missed) != 2 {
		t.Errorf("want 2 misses after dedup, got %d: %v", len(got.Missed), got.Missed)
	}
	if !got.RetrievedAt.Equal(parseTime(t, "2026-08-18T11:00:00Z")) {
		t.Errorf("the clock is not the latest read: %v", got.RetrievedAt)
	}
	if strings.Join(got.Unread, ",") != "imageBlock" {
		t.Errorf("unread: %v", got.Unread)
	}
}

// TestMergeTakesTheStrictestRobotsNote: one disallowed read in a batch is the
// fact worth reporting, not the four that were allowed.
func TestMergeTakesTheStrictestRobotsNote(t *testing.T) {
	allowed := Envelope{Robots: &RobotsNote{Allowed: true}}
	refused := Envelope{Robots: &RobotsNote{Allowed: false, Rule: "Disallow: /gp/offer-listing/"}}
	for _, order := range [][]Envelope{{allowed, refused}, {refused, allowed}} {
		got := mergeEnvelopes(nil, order)
		if got.Robots == nil || got.Robots.Allowed {
			t.Fatalf("a refusal was lost: %+v", got.Robots)
		}
		if got.Robots.Rule == "" {
			t.Error("the note does not name the rule")
		}
	}
}

// TestMergeIgnoresARecordWithNoEnvelope: a `query` row is a local read and there
// is no page behind it, which is not an error.
func TestMergeIgnoresARecordWithNoEnvelope(t *testing.T) {
	got := mergeEnvelopes([]json.RawMessage{json.RawMessage(`{"asin":"B0"}`), json.RawMessage(`not json`)}, nil)
	if len(got.Surfaces) != 0 || len(got.Missed) != 0 {
		t.Errorf("a record with no envelope contributed something: %+v", got)
	}
	if got.Missed == nil {
		t.Error("missed is nil rather than empty")
	}
}

// TestPageEnvelopesReachTheResult is the row shaped case: reviews, offers,
// price, variants, refine and tree emit rows that carry no envelope of their
// own, and the page's has to arrive some other way.
func TestPageEnvelopesReachTheResult(t *testing.T) {
	page := Envelope{Marketplace: "us", Surfaces: []string{"s1"},
		Missed: []Miss{{Field: "reviews", Why: "behind a sign-in", Have: 2, Total: 284512}}}
	srv := stub(t, Records{
		Rows:  []json.RawMessage{json.RawMessage(`{"review_id":"R1"}`), json.RawMessage(`{"review_id":"R2"}`)},
		Pages: []Envelope{page},
	}, nil)
	res, err := srv.Call(t.Context(), "reviews", map[string]any{"asin": "B0"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 2 {
		t.Fatalf("want 2 records, got %d", res.Count)
	}
	if len(res.Envelope.Missed) != 1 || res.Envelope.Missed[0].Total != 284512 {
		t.Fatalf("the page envelope did not reach the result: %+v", res.Envelope)
	}
}

func TestStatusForMapsTheExitCodes(t *testing.T) {
	want := map[int]int{
		0: http.StatusOK,
		1: http.StatusInternalServerError,
		2: http.StatusBadRequest,
		5: http.StatusForbidden,
		6: http.StatusTooManyRequests,
		7: http.StatusForbidden,
		8: http.StatusServiceUnavailable,
		9: http.StatusForbidden,
	}
	for code, status := range want {
		if got := statusFor(code); got != status {
			t.Errorf("exit %d: want %d, got %d", code, status, got)
		}
	}
}

// TestUnknownToolNeverReachesTheDispatcher: the registry is the allowlist, so a
// name that is not in it must not become a run.
func TestUnknownToolNeverReachesTheDispatcher(t *testing.T) {
	called := false
	srv := stub(t, Records{}, nil)
	srv.Dispatch = func(context.Context, string, map[string]any) (Records, error) {
		called = true
		return Records{}, nil
	}
	if _, err := srv.Call(t.Context(), "crawl", nil); err == nil {
		t.Fatal("crawl was accepted")
	}
	if called {
		t.Fatal("the dispatcher ran for a tool that is not in the registry")
	}

	h := httptest.NewServer(srv.Handler())
	defer h.Close()
	resp, err := h.Client().Get(h.URL + "/v1/tools/crawl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
	if called {
		t.Fatal("the dispatcher ran over HTTP for a tool that is not in the registry")
	}
}

// TestQueryStringArgumentsAreTyped: a URL carries strings and nothing else, so a
// boolean and a repeated argument have to be recovered from the tool's own
// schema rather than left for the caller to spell right.
func TestQueryStringArgumentsAreTyped(t *testing.T) {
	var got map[string]any
	srv := stub(t, Records{}, nil)
	srv.Dispatch = func(_ context.Context, _ string, args map[string]any) (Records, error) {
		got = args
		return Records{}, nil
	}
	h := httptest.NewServer(srv.Handler())
	defer h.Close()
	resp, err := h.Client().Get(h.URL + "/v1/tools/reviews?asin=B0&verified&refine=a&refine=b")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got["asin"] != "B0" {
		t.Errorf("asin came through as %#v", got["asin"])
	}
	if got["verified"] != true {
		t.Errorf("a valueless flag did not become true: %#v", got["verified"])
	}
	list, ok := got["refine"].([]any)
	if !ok || len(list) != 2 {
		t.Errorf("a repeated argument did not become a list: %#v", got["refine"])
	}
}

// mcpRun drives the transport over a pair of buffers.
func mcpRun(t *testing.T, srv *Server, lines ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	m := &MCP{Server: srv, In: strings.NewReader(strings.Join(lines, "\n") + "\n"), Out: &out}
	if err := m.Serve(t.Context()); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("a response is not JSON: %v\n%s", err, line)
		}
		got = append(got, v)
	}
	return got
}

// TestMCPNotificationsAreNeverAnswered: replying to a message with no id is the
// most common way to break a client.
func TestMCPNotificationsAreNeverAnswered(t *testing.T) {
	got := mcpRun(t, stub(t, Records{}, nil),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	)
	if len(got) != 1 {
		t.Fatalf("want 1 response for 2 notifications and 1 request, got %d: %v", len(got), got)
	}
	if got[0]["id"] != float64(7) {
		t.Errorf("the response is for id %v", got[0]["id"])
	}
}

func TestMCPRejectsAMessageWithoutTheVersion(t *testing.T) {
	got := mcpRun(t, stub(t, Records{}, nil), `{"id":1,"method":"ping"}`)
	e, _ := got[0]["error"].(map[string]any)
	if e == nil || e["code"] != float64(rpcInvalidRequest) {
		t.Fatalf("want an invalid request error, got %v", got[0])
	}
}

// TestMCPReportsAToolFailureInBand: a CAPTCHA or a robots refusal is an answer
// the model should read, not a transport fault it never sees.
func TestMCPReportsAToolFailureInBand(t *testing.T) {
	srv := stub(t, Records{}, &ToolError{Code: 5, Err: errors.New("amazon answered with a CAPTCHA. run `amz why captcha`")})
	got := mcpRun(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reviews","arguments":{"asin":"B0"}}}`)
	res, _ := got[0]["result"].(map[string]any)
	if res == nil {
		t.Fatalf("a tool failure became a transport error: %v", got[0])
	}
	if res["isError"] != true {
		t.Errorf("the failure is not marked isError: %v", res)
	}
	content, _ := res["content"].([]any)
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	if !strings.Contains(text, "exit 5") {
		t.Errorf("the message does not carry the exit code: %q", text)
	}
}

// TestMCPToolListIsTheRegistry checks the schema a model actually reads.
func TestMCPToolListIsTheRegistry(t *testing.T) {
	got := mcpRun(t, stub(t, Records{}, nil), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res, _ := got[0]["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
	tool, _ := tools[0].(map[string]any)
	schema, _ := tool["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Error("the schema allows arguments the tool does not have")
	}
	props, _ := schema["properties"].(map[string]any)
	verified, _ := props["verified"].(map[string]any)
	if verified["type"] != "boolean" {
		t.Errorf("verified is typed %v", verified["type"])
	}
	refine, _ := props["refine"].(map[string]any)
	if refine["type"] != "array" {
		t.Errorf("a repeated argument is typed %v rather than array", refine["type"])
	}
	req, _ := schema["required"].([]any)
	if len(req) != 1 || req[0] != "asin" {
		t.Errorf("required is %v", req)
	}
}

func TestMCPBannerAndInstructionsSayWhatIsRefused(t *testing.T) {
	m := &MCP{Server: stub(t, Records{}, nil)}
	if !strings.Contains(m.Banner(), NoRobotsNotice) {
		t.Errorf("the banner does not carry the notice:\n%s", m.Banner())
	}
	got := mcpRun(t, m.Server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	res, _ := got[0]["result"].(map[string]any)
	if res["protocolVersion"] != MCPProtocolVersion {
		t.Errorf("protocol version is %v", res["protocolVersion"])
	}
	instructions, _ := res["instructions"].(string)
	for _, want := range []string{"missed", NoRobotsNotice} {
		if !strings.Contains(instructions, want) {
			t.Errorf("the instructions do not mention %q:\n%s", want, instructions)
		}
	}
}
