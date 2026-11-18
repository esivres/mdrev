package preview

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/esivres/mdrev/internal/mrsf"
)

const fixture = `# Heading

First paragraph lives here.

Second paragraph makes a claim.
`

// A review a foreign tool could have left behind: ids are not uuids, one
// thread is already closed.
const seededReview = `mrsf_version: "1.0"
document: doc.md
comments:
  - id: aaa1
    author: bob
    text: what does this mean?
    line: 3
    selected_text: First paragraph lives here.
  - id: zzz1
    author: bob
    text: settled earlier
    line: 5
    resolved: true
`

func writeDocument(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	document := writeDocument(t, fixture)
	server, err := New(document)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return server, ts, document
}

// seedReview writes a sidecar directly, the way a foreign tool would have.
func seedReview(t *testing.T, document, review string) {
	t.Helper()
	if err := os.WriteFile(mrsf.Path(document), []byte(review), 0o644); err != nil {
		t.Fatal(err)
	}
}

func post(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

// A comment written from the page must land in the sidecar exactly as
// `mdrev comment` would have recorded it, or the CLI and agents would read a
// different review than the page shows.
func TestCommentHandlerRecordsQuoteAndType(t *testing.T) {
	_, ts, document := newTestServer(t)

	code, body := post(t, ts.URL+"/api/comment", map[string]any{
		"text": "Fix this", "type": "issue", "quote": "the claim", "line": 4, "author": "alice",
	})
	if code != http.StatusOK || body["ok"] != true {
		t.Fatalf("comment rejected: %d %v", code, body)
	}

	sc, err := mrsf.Load(document)
	if err != nil || sc == nil {
		t.Fatalf("sidecar after comment: %v, %v", sc, err)
	}
	got := sc.Threads(true)
	if len(got) != 1 {
		t.Fatalf("want one thread, got %d", len(got))
	}
	c := got[0].Parent
	if c.Author != "alice" || c.Text != "Fix this" || c.Type != "issue" ||
		c.Line != 4 || c.SelectedText != "the claim" {
		t.Errorf("comment recorded as %+v", c)
	}
	if c.ID == "" || c.Timestamp == "" {
		t.Errorf("Add must fill id and timestamp, got %+v", c)
	}
}

// Without a selection the comment anchors on the start of the line, or it
// could not follow the text at all.
func TestCommentHandlerAnchorsOnTheLineWithoutQuote(t *testing.T) {
	_, ts, document := newTestServer(t)

	code, body := post(t, ts.URL+"/api/comment", map[string]any{"text": "Tighten", "line": 3})
	if code != http.StatusOK || body["ok"] != true {
		t.Fatalf("comment rejected: %d %v", code, body)
	}

	sc, err := mrsf.Load(document)
	if err != nil || sc == nil {
		t.Fatal(err)
	}
	c := sc.Threads(true)[0].Parent
	if c.SelectedText != "First paragraph lives here." {
		t.Errorf("want the line quoted, got %q", c.SelectedText)
	}
	if c.Author != mrsf.DefaultAuthor() {
		t.Errorf("want the default author %q, got %q", mrsf.DefaultAuthor(), c.Author)
	}
}

// A reply inherits the parent's anchor and never carries one of its own.
func TestReplyHandlerNestsUnderParent(t *testing.T) {
	_, ts, document := newTestServer(t)
	seedReview(t, document, seededReview)

	code, body := post(t, ts.URL+"/api/reply", map[string]any{"id": "aaa", "text": "It means X"})
	if code != http.StatusOK || body["ok"] != true {
		t.Fatalf("reply rejected: %d %v", code, body)
	}

	sc, err := mrsf.Load(document)
	if err != nil {
		t.Fatal(err)
	}
	// Open threads only: the seeded review carries a resolved one besides.
	threads := sc.Threads(false)
	if len(threads) != 1 || len(threads[0].Replies) != 1 {
		t.Fatalf("want one thread with one reply, got %+v", threads)
	}
	r := threads[0].Replies[0]
	if r.ReplyTo != "aaa1" || r.Line != 3 || r.SelectedText != "First paragraph lives here." {
		t.Errorf("reply recorded as %+v", r)
	}
}

// Resolving records how the thread ended, or an agent cannot tell settled
// from dismissed and proposes again.
func TestResolveHandlerClosesTheThread(t *testing.T) {
	_, ts, document := newTestServer(t)
	seedReview(t, document, seededReview)

	code, body := post(t, ts.URL+"/api/resolve", map[string]any{"id": "aaa"})
	if code != http.StatusOK || body["ok"] != true {
		t.Fatalf("resolve rejected: %d %v", code, body)
	}

	sc, err := mrsf.Load(document)
	if err != nil {
		t.Fatal(err)
	}
	c := sc.Find("aaa1")
	if c == nil || !c.Resolved || c.Outcome() != mrsf.OutcomeResolved {
		t.Errorf("thread not closed properly: %+v", c)
	}
	// The already-resolved thread must come out untouched.
	if z := sc.Find("zzz1"); z == nil || z.Outcome() != "" {
		t.Errorf("resolved thread disturbed: %+v", z)
	}
}

// The page shows open threads by default; ?all=1 must reach the resolved ones.
func TestThreadsEndpointSeparatesResolved(t *testing.T) {
	_, ts, document := newTestServer(t)
	seedReview(t, document, seededReview)

	get := func(url string) threadsPayload {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out threadsPayload
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := get(ts.URL + "/api/threads"); len(got.Threads) != 1 {
		t.Errorf("resolved threads must be hidden by default, got %d", len(got.Threads))
	}
	got := get(ts.URL + "/api/threads?all=1")
	if len(got.Threads) != 2 {
		t.Errorf("want both threads with all=1, got %d", len(got.Threads))
	}
	if len(got.Threads) > 1 && len(got.Threads[0].Replies) != 0 {
		t.Errorf("thread with no replies must carry none, got %+v", got.Threads[0])
	}
}

// Rejected writes must answer 4xx with an explanation and leave the review
// alone.
func TestHandlersRejectBadRequests(t *testing.T) {
	_, ts, document := newTestServer(t)
	seedReview(t, document, seededReview)

	tests := []struct {
		name string
		path string
		body map[string]any
	}{
		{"empty text", "/api/comment", map[string]any{"text": "   ", "line": 1}},
		{"unknown type", "/api/comment", map[string]any{"text": "x", "type": "nit", "line": 1}},
		{"negative line", "/api/comment", map[string]any{"text": "x", "line": -2}},
		{"unknown reply id", "/api/reply", map[string]any{"id": "nomatch", "text": "x"}},
		{"empty reply", "/api/reply", map[string]any{"id": "aaa", "text": ""}},
		{"unknown resolve id", "/api/resolve", map[string]any{"id": "nomatch"}},
	}
	for _, test := range tests {
		code, body := post(t, ts.URL+test.path, test.body)
		if code < 400 || code > 499 {
			t.Errorf("%s: want 4xx, got %d", test.name, code)
		}
		if body["ok"] != false || body["error"] == "" {
			t.Errorf("%s: want ok:false with an error, got %v", test.name, body)
		}
	}

	// Nothing above may have reached the sidecar.
	sc, err := mrsf.Load(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Comments) != 2 {
		t.Errorf("rejected requests wrote anyway: %d comments", len(sc.Comments))
	}
}
