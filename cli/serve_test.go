package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamnd/amz-cli/amz"
)

// serveApp is the App a test server runs with. Rate is zero because the fixture
// server is local and there is nobody to be polite to.
func serveApp(t *testing.T) *App {
	t.Helper()
	return &App{Marketplace: "us", Rate: 0, Retries: 1, Timeout: 10 * time.Second}
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	fixtureServer(t)
	h := httptest.NewServer(newServer(serveApp(t)).Handler())
	t.Cleanup(h.Close)
	return h
}

// get calls one tool over HTTP and decodes the result.
func get(t *testing.T, h *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := h.Client().Get(h.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s answered with something that is not JSON: %v\n%s", path, err, b)
	}
	return resp.StatusCode, v
}

// TestMCPHasNoOverride is the test the spec names.
//
// --no-robots has to be unreachable through the server in four separate ways,
// because one of them alone is a policy and four of them is a guarantee: it is
// not an argument of any tool, an attempt to pass it is a usage error rather
// than something ignored, it cannot survive argv construction, and the server
// says all of that out loud where a caller will see it.
func TestMCPHasNoOverride(t *testing.T) {
	for _, tool := range toolRegistry() {
		for _, a := range tool.Args {
			if strings.Contains(a.Name, "robot") {
				t.Fatalf("tool %s declares an argument %q", tool.Name, a.Name)
			}
		}
	}

	d := &dispatcher{marketplace: "us"}
	spec, ok := specFor("product")
	if !ok {
		t.Fatal("no product tool")
	}
	// The underscore spelling is assembled rather than written out, because
	// TestNoRobotsNotInConfig scans this repo for that literal and is right to.
	for _, spelling := range []string{"no-robots", "no" + "_robots", "noRobots", "robots"} {
		_, err := d.argvFor(spec, map[string]any{"asin": "B08N5WRWNW", spelling: true})
		if err == nil {
			t.Fatalf("%q was accepted as an argument", spelling)
		}
		if codeFor(err) != CodeUsage {
			t.Fatalf("%q: want exit %d, got %d (%v)", spelling, CodeUsage, codeFor(err), err)
		}
	}

	// Nothing a caller can put in a value becomes a flag either, because the
	// positional arguments go after --.
	argv, err := d.argvFor(spec, map[string]any{"asin": "--no-robots"})
	if err != nil {
		t.Fatal(err)
	}
	dash := -1
	for i, a := range argv {
		if a == "--" {
			dash = i
		}
		if a == "--no-robots" && (dash == -1 || i < dash) {
			t.Fatalf("--no-robots landed in the flag half of the argv: %v", argv)
		}
	}

	// And the server says why, in the banner and in the help.
	m := &amz.MCP{Server: newServer(serveApp(t))}
	if !strings.Contains(m.Banner(), "--no-robots") {
		t.Errorf("the startup banner does not mention --no-robots:\n%s", m.Banner())
	}
	help, _ := run(t, "mcp", "--help")
	if !strings.Contains(help, "--no-robots has no effect here") {
		t.Errorf("mcp --help does not say --no-robots has no effect:\n%s", help)
	}
	serveHelp, _ := run(t, "serve", "--help")
	if !strings.Contains(serveHelp, "--no-robots has no effect here") {
		t.Errorf("serve --help does not say --no-robots has no effect:\n%s", serveHelp)
	}
}

// TestNoRobotsCannotReachARun is the belt to that test's braces: even if a spec
// were edited to declare the flag, the dispatcher refuses the run.
func TestNoRobotsCannotReachARun(t *testing.T) {
	d := &dispatcher{marketplace: "us", inherited: []string{"--no-robots"}}
	_, err := d.run(context.Background(), "lookup", map[string]any{"uri": "B08N5WRWNW"})
	if err == nil {
		t.Fatal("a --no-robots argv ran anyway")
	}
	if !strings.Contains(err.Error(), "--no-robots") {
		t.Fatalf("the refusal does not name the flag: %v", err)
	}
}

// TestServerExposesExactlyTheReadTools pins the registry to the list in the
// spec. Adding a tool is a decision, so it should be a diff here too.
func TestServerExposesExactlyTheReadTools(t *testing.T) {
	want := []string{
		"product", "price", "offers", "reviews", "qa", "variants", "related",
		"search", "refine", "category", "tree", "brand", "seller", "author",
		"bestsellers", "new-releases", "movers", "wished", "gifted", "deals",
		"lookup", "find", "query", "series", "graph",
	}
	got := make([]string, 0, len(want))
	for _, tool := range toolRegistry() {
		got = append(got, tool.Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("registry drifted\n want %v\n  got %v", want, got)
	}
}

// TestWriteCommandsAreNotExposed checks the deny list from the other side.
func TestWriteCommandsAreNotExposed(t *testing.T) {
	denied := append([]string{"seed", "cache", "doctor", "db"}, notExposed...)
	for _, name := range denied {
		head := strings.Fields(name)[0]
		if _, ok := specFor(head); ok {
			t.Errorf("%s is exposed as a tool", head)
		}
	}
	// search --enqueue writes to the crawl queue, so it is off the wire for the
	// same reason crawl is, even though search itself is a read tool.
	spec, _ := specFor("search")
	if _, ok := spec.arg("enqueue"); ok {
		t.Error("search exposes --enqueue, which enqueues a crawl")
	}
}

// TestUnknownToolIsNotACommand makes sure the allowlist is the only door. A
// caller that spells a real command that is not a tool gets a 404 and not a run.
func TestUnknownToolIsNotACommand(t *testing.T) {
	h := testServer(t)
	for _, name := range []string{"crawl", "seed", "export", "config", "open", "nonsense"} {
		status, body := get(t, h, "/v1/tools/"+name)
		if status != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d (%v)", name, status, body)
		}
	}
}

// TestEveryToolMatchesARealCommand is the drift test. Every tool name has to be
// a command in the tree and every argument has to be a flag on it, with a type
// that agrees, or the schema a model reads is fiction.
func TestEveryToolMatchesARealCommand(t *testing.T) {
	root := Root()
	for _, s := range toolSpecs() {
		cmd, _, err := root.Find(s.path)
		if err != nil || cmd == root {
			t.Errorf("tool %s: no command %v in the tree", s.name, s.path)
			continue
		}
		for _, a := range s.flags {
			f := cmd.Flags().Lookup(a.Name)
			if f == nil {
				f = root.PersistentFlags().Lookup(a.Name)
			}
			if f == nil {
				t.Errorf("tool %s: no flag --%s on %s", s.name, a.Name, cmd.Name())
				continue
			}
			kind := f.Value.Type()
			switch {
			case a.Type == "boolean" && kind != "bool":
				t.Errorf("tool %s: --%s is %s, declared boolean", s.name, a.Name, kind)
			case a.Type == "integer" && kind != "int":
				t.Errorf("tool %s: --%s is %s, declared integer", s.name, a.Name, kind)
			case a.Repeated && kind != "stringSlice" && kind != "stringArray":
				t.Errorf("tool %s: --%s is %s, declared repeated", s.name, a.Name, kind)
			case a.Type == "string" && !a.Repeated && kind != "string":
				t.Errorf("tool %s: --%s is %s, declared string", s.name, a.Name, kind)
			}
		}
	}
}

// TestArgvIsBuiltFromTheTable spells out what a call turns into, because that
// string is the whole security boundary.
func TestArgvIsBuiltFromTheTable(t *testing.T) {
	d := &dispatcher{marketplace: "us", inherited: []string{"--rate=0s"}}
	spec, _ := specFor("search")
	argv, err := d.argvFor(spec, map[string]any{
		"query":             "usb c cable",
		"page":              float64(2),
		"pages":             false,
		"sort":              "price-asc",
		"refine":            []any{"p_123=1", "p_89=2"},
		"include-sponsored": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	for _, want := range []string{
		"search", "--sort=price-asc", "--refine=p_123=1", "--refine=p_89=2",
		"--page=2", "--include-sponsored", "--marketplace=us", "--rate=0s",
		"--output=json", "-- usb c cable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("argv is missing %q:\n%s", want, got)
		}
	}
	// A false boolean is absent, not --pages=false.
	if strings.Contains(got, "--pages") {
		t.Errorf("a false boolean became a flag:\n%s", got)
	}
}

func TestArgvRejectsBadValues(t *testing.T) {
	d := &dispatcher{marketplace: "us"}
	search, _ := specFor("search")
	product, _ := specFor("product")
	cases := []struct {
		name string
		spec toolSpec
		args map[string]any
		want string
	}{
		{"missing required", search, map[string]any{"sort": "review"}, `needs "query"`},
		{"unknown argument", search, map[string]any{"query": "x", "workers": 8}, `no argument "workers"`},
		{"bad enum", search, map[string]any{"query": "x", "sort": "cheapest"}, "is not one of"},
		{"fractional integer", search, map[string]any{"query": "x", "page": 1.5}, "whole number"},
		{"list for a scalar", search, map[string]any{"query": "x", "sort": []any{"review", "newest"}}, "takes one value"},
		{"string for a boolean", product, map[string]any{"asin": "B0", "light": "yes"}, "true/false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := d.argvFor(tc.spec, tc.args)
			if err == nil {
				t.Fatal("accepted")
			}
			if codeFor(err) != CodeUsage {
				t.Errorf("want exit %d, got %d", CodeUsage, codeFor(err))
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want a message containing %q, got %q", tc.want, err)
			}
		})
	}
}

// TestRepeatedArgumentTakesOneOrMany lets a caller send a scalar where a list is
// allowed, because a model that sends "predicate": "variant_of" meant one.
func TestRepeatedArgumentTakesOneOrMany(t *testing.T) {
	d := &dispatcher{marketplace: "us"}
	spec, _ := specFor("graph")
	argv, err := d.argvFor(spec, map[string]any{"uri": "B08N5WRWNW", "predicate": "variant_of"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(argv, " "), "--predicate=variant_of") {
		t.Fatalf("a scalar was not accepted for a repeated argument: %v", argv)
	}
}

// TestEveryResultCarriesTheEnvelope is the reason the server exists in this
// shape. A model that gets eight reviews has to be told what it did not get.
func TestEveryResultCarriesTheEnvelope(t *testing.T) {
	h := testServer(t)
	status, body := get(t, h, "/v1/tools/reviews?asin=B08N5WRWNW")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d: %v", status, body)
	}
	env, ok := body["envelope"].(map[string]any)
	if !ok {
		t.Fatalf("no envelope on the result: %v", body)
	}
	if _, ok := env["missed"]; !ok {
		t.Errorf("the envelope has no missed key: %v", env)
	}
	if env["marketplace"] != "us" {
		t.Errorf("envelope marketplace is %v", env["marketplace"])
	}
	if sf, _ := env["surfaces"].([]any); len(sf) == 0 {
		t.Errorf("the envelope names no surfaces: %v", env)
	}
	if _, ok := body["count"]; !ok {
		t.Errorf("no count on the result: %v", body)
	}

	// The sentence this whole design is for. A model that gets two reviews has
	// to be told how many there are, and the count only exists in the envelope
	// because a review row deliberately does not carry one.
	var missed []amz.Miss
	b, err := json.Marshal(env["missed"])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &missed); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range missed {
		if m.Field == "reviews" {
			found = true
			if m.Total <= int64(body["count"].(float64)) {
				t.Errorf("reviews reports %d of %d, which says nothing was missed", m.Have, m.Total)
			}
			if m.Why == "" {
				t.Error("the miss does not say why")
			}
		}
	}
	if !found {
		t.Errorf("a reviews call that returned %v rows carries no missed entry for reviews: %v", body["count"], missed)
	}
}

// TestMissedIsNeverOmitted is the serialization half of that. An absent key and
// an empty list must not be the same bytes.
func TestMissedIsNeverOmitted(t *testing.T) {
	res := &amz.Result{Tool: "query", Records: []json.RawMessage{}, Envelope: amz.ResultEnvelope{Missed: []amz.Miss{}}}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"missed":[]`)) {
		t.Fatalf("missed was omitted from an empty envelope:\n%s", b)
	}
}

// TestNoDataIsAnAnswer keeps exit 3 off the wire. A tool that read the page and
// found nothing succeeded, and the envelope already says what was missing.
func TestNoDataIsAnAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMZ_DATA_DIR", dir)
	app := serveApp(t)
	app.DataDir = dir
	h := httptest.NewServer(newServer(app).Handler())
	t.Cleanup(h.Close)

	status, body := get(t, h, "/v1/tools/find?text=widget")
	if status != http.StatusOK {
		t.Fatalf("want 200 for an empty result, got %d: %v", status, body)
	}
	if body["count"] != float64(0) {
		t.Errorf("want count 0, got %v", body["count"])
	}
	if _, isErr := body["error"]; isErr {
		t.Errorf("an empty result came back as an error: %v", body)
	}
}

func TestUsageErrorIs400(t *testing.T) {
	h := testServer(t)
	status, body := get(t, h, "/v1/tools/search?query=x&sort=cheapest")
	if status != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %v", status, body)
	}
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error block: %v", body)
	}
	if e["exit"] != float64(CodeUsage) {
		t.Errorf("want exit %d, got %v", CodeUsage, e["exit"])
	}
}

func TestMissingRequiredArgumentIs400(t *testing.T) {
	h := testServer(t)
	status, _ := get(t, h, "/v1/tools/product")
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for a product call with no asin, got %d", status)
	}
}

// TestQueryStringSpellsFlagsTheWayAURLDoes: ?verified with no value is how a URL
// writes a flag, and a model handing us ?verified=true meant the same thing.
func TestQueryStringSpellsFlagsTheWayAURLDoes(t *testing.T) {
	h := testServer(t)
	for _, q := range []string{"", "&verified", "&verified=true", "&verified=false"} {
		status, body := get(t, h, "/v1/tools/reviews?asin=B08N5WRWNW"+q)
		if status != http.StatusOK {
			t.Errorf("%q: want 200, got %d: %v", q, status, body)
		}
	}
}

// TestRemovedFlagsAreNotArguments: `search --prime` still exists so that a
// v0.2.1 script is told what happened, but it only ever returns an error now.
// An argument whose whole behaviour is to fail has no business in a schema a
// model reads.
func TestRemovedFlagsAreNotArguments(t *testing.T) {
	for _, tool := range []string{"search", "refine"} {
		spec, _ := specFor(tool)
		if _, ok := spec.arg("prime"); ok {
			t.Errorf("%s exposes --prime, which is gone in v0.3.0 and only returns an error", tool)
		}
	}
	// offers --prime is a different flag with the same name and it works, so it
	// stays.
	offers, _ := specFor("offers")
	if _, ok := offers.arg("prime"); !ok {
		t.Error("offers should still expose --prime")
	}
}

func TestPostTakesAJSONBody(t *testing.T) {
	h := testServer(t)
	body := strings.NewReader(`{"asin":"B08N5WRWNW","depth":"meta"}`)
	resp, err := h.Client().Post(h.URL+"/v1/tools/product", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, b)
	}
	var res amz.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.Count == 0 {
		t.Fatal("no records")
	}
}

func TestToolsListingAndIndex(t *testing.T) {
	h := testServer(t)
	status, body := get(t, h, "/v1/tools")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d", status)
	}
	if body["count"] != float64(25) {
		t.Errorf("want 25 tools, got %v", body["count"])
	}
	status, index := get(t, h, "/")
	if status != http.StatusOK {
		t.Fatalf("index: want 200, got %d", status)
	}
	notice, _ := index["robots"].(string)
	if !strings.Contains(notice, "--no-robots has no effect here") {
		t.Errorf("the index does not carry the robots notice: %v", index["robots"])
	}
	status, health := get(t, h, "/healthz")
	if status != http.StatusOK || health["status"] != "ok" {
		t.Errorf("healthz answered %d %v", status, health)
	}
}

// TestServeAnswersOneCallAtATime is the reason --workers was removed, applied to
// the server. Two requests in flight make --rate a lie by a factor of two, and
// a server that fanned out would undo the whole pacing discipline quietly.
func TestServeAnswersOneCallAtATime(t *testing.T) {
	var inflight, peak int64
	var mu sync.Mutex
	page, err := os.ReadFile(filepath.Join("..", "amz", "testdata", "product.html"))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&inflight, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&inflight, -1)
		_, _ = w.Write(page)
	}))
	defer upstream.Close()
	t.Setenv("AMZ_BASE_URL", upstream.URL)
	t.Setenv("AMZ_CACHE_DIR", t.TempDir())

	h := httptest.NewServer(newServer(serveApp(t)).Handler())
	defer h.Close()

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := h.Client().Get(h.URL + "/v1/tools/price?asin=B08N5WRWN" + string(rune('0'+i)))
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}(i)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if peak > 1 {
		t.Fatalf("%d requests were in flight at once, the server is meant to read one page at a time", peak)
	}
}

// TestServeRefusesAPublicBind: binding every interface makes anyone who reaches
// the port a crawler using this machine's address, which is worth typing out.
func TestServeRefusesAPublicBind(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8787", ":8787", "192.0.2.10:8787"} {
		if err := checkBind(addr, false); err == nil {
			t.Errorf("%s was accepted without --yes", addr)
		} else if codeFor(err) != CodeUsage {
			t.Errorf("%s: want exit %d, got %d", addr, CodeUsage, codeFor(err))
		}
		if err := checkBind(addr, true); err != nil {
			t.Errorf("%s was refused even with --yes: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:8787", "localhost:0", "[::1]:8787"} {
		if err := checkBind(addr, false); err != nil {
			t.Errorf("%s was refused: %v", addr, err)
		}
	}
}

// mcpRoundTrip drives the stdio protocol through the real command, without a
// subprocess. It goes through the cobra tree rather than straight to amz.MCP so
// that the wiring in mcpCmd is under test too.
func mcpRoundTrip(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	root := Root()
	root.SetArgs([]string{"--rate", "0", "mcp"})
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetIn(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err := root.Execute(); err != nil {
		t.Fatalf("amz mcp: %v", err)
	}
	var got []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("a response line is not JSON: %v\n%s", err, line)
		}
		got = append(got, v)
	}
	return got
}

func TestMCPSpeaksTheProtocol(t *testing.T) {
	fixtureServer(t)
	got := mcpRoundTrip(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)
	// The notification is handled and never answered. Three requests, three
	// responses.
	if len(got) != 3 {
		t.Fatalf("want 3 responses for 3 requests and 1 notification, got %d", len(got))
	}
	init, _ := got[0]["result"].(map[string]any)
	if init["protocolVersion"] != amz.MCPProtocolVersion {
		t.Errorf("initialize answered protocol %v", init["protocolVersion"])
	}
	if info, _ := init["serverInfo"].(map[string]any); info["name"] != "amz" {
		t.Errorf("serverInfo is %v", init["serverInfo"])
	}
	instructions, _ := init["instructions"].(string)
	if !strings.Contains(instructions, "--no-robots has no effect here") {
		t.Errorf("initialize instructions do not carry the robots notice: %q", instructions)
	}
	list, _ := got[1]["result"].(map[string]any)
	tools, _ := list["tools"].([]any)
	if len(tools) != 25 {
		t.Fatalf("tools/list returned %d tools", len(tools))
	}
	first, _ := tools[0].(map[string]any)
	schema, _ := first["inputSchema"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("the first tool has no object input schema: %v", first)
	}
	if req, _ := schema["required"].([]any); len(req) == 0 {
		t.Errorf("product declares no required argument: %v", schema)
	}
	if ann, _ := first["annotations"].(map[string]any); ann["readOnlyHint"] != true {
		t.Errorf("a read tool is not marked read only: %v", first["annotations"])
	}
}

func TestMCPToolCallCarriesTheEnvelope(t *testing.T) {
	fixtureServer(t)
	got := mcpRoundTrip(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reviews","arguments":{"asin":"B08N5WRWNW"}}}`)
	if len(got) != 1 {
		t.Fatalf("want 1 response, got %d", len(got))
	}
	res, _ := got[0]["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("the call failed: %v", res)
	}
	content, _ := res["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("want one content block, got %v", content)
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	var payload amz.Result
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("the content block is not a result: %v\n%s", err, text)
	}
	if payload.Tool != "reviews" {
		t.Errorf("the result names tool %q", payload.Tool)
	}
	if payload.Envelope.Missed == nil {
		t.Error("missed is null rather than a list")
	}
	if len(payload.Envelope.Surfaces) == 0 {
		t.Error("the envelope names no surfaces")
	}
}

func TestMCPReportsAFailedToolAsAResult(t *testing.T) {
	fixtureServer(t)
	got := mcpRoundTrip(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"x","sort":"cheapest"}}}`)
	res, _ := got[0]["result"].(map[string]any)
	if res == nil {
		t.Fatalf("a tool failure became a transport error: %v", got[0])
	}
	if res["isError"] != true {
		t.Errorf("a failed call is not marked isError: %v", res)
	}
}

func TestMCPRejectsWhatItShould(t *testing.T) {
	got := mcpRoundTrip(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"crawl","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3}`,
		`not json at all`,
	)
	if len(got) != 4 {
		t.Fatalf("want 4 responses, got %d: %v", len(got), got)
	}
	for i, want := range []float64{-32602, -32601, -32601, -32700} {
		e, _ := got[i]["error"].(map[string]any)
		if e == nil {
			t.Errorf("response %d is not an error: %v", i, got[i])
			continue
		}
		if e["code"] != want {
			t.Errorf("response %d: want code %v, got %v (%v)", i, want, e["code"], e["message"])
		}
	}
	if msg, _ := got[0]["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "crawl") {
		t.Errorf("the refusal does not name the tool: %v", got[0])
	}
}

// TestToolsFlagPrintsTheRegistry: an operator should be able to see what the
// thing exposes without speaking JSON-RPC at it.
func TestToolsFlagPrintsTheRegistry(t *testing.T) {
	for _, cmd := range []string{"mcp", "serve"} {
		out, err := run(t, cmd, "--tools", "-o", "json")
		if err != nil {
			t.Fatalf("%s --tools: %v", cmd, err)
		}
		var tools []struct {
			Tool string `json:"tool"`
		}
		if err := json.Unmarshal([]byte(out), &tools); err != nil {
			t.Fatalf("%s --tools is not JSON: %v\n%s", cmd, err, out)
		}
		if len(tools) != 25 {
			t.Errorf("%s --tools listed %d tools", cmd, len(tools))
		}
		for _, tool := range tools {
			for _, denied := range notExposed {
				if tool.Tool == strings.Fields(denied)[0] {
					t.Errorf("%s --tools lists %s", cmd, denied)
				}
			}
		}
	}
}

// TestListingFlagsComeBackAsText covers the two flags that answer with a listing
// rather than records. A truthful single record beats a parse error.
func TestListingFlagsComeBackAsText(t *testing.T) {
	h := testServer(t)
	status, body := get(t, h, "/v1/tools/graph?uri=B08N5WRWNW&predicates=true")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d: %v", status, body)
	}
	recs, _ := body["records"].([]any)
	if len(recs) != 1 {
		t.Fatalf("want one record holding the listing, got %d", len(recs))
	}
	rec, _ := recs[0].(map[string]any)
	text, _ := rec["text"].(string)
	if !strings.Contains(text, "variant_of") {
		t.Errorf("the predicate listing did not come through: %q", text)
	}
}

func TestServeAndMCPAreNotInTheRegistry(t *testing.T) {
	// A server that exposed itself as a tool would let a model start another one.
	for _, name := range []string{"serve", "mcp"} {
		if _, ok := specFor(name); ok {
			t.Errorf("%s is exposed as a tool", name)
		}
	}
}
