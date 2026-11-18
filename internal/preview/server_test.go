package preview

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The page is fed the raw document it renders.
func TestContentEndpointReturnsTheRawDocument(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/content")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Text != fixture {
		t.Errorf("want the document verbatim, got %q", out.Text)
	}
	if !strings.HasSuffix(out.Path, "doc.md") {
		t.Errorf("want the document path, got %q", out.Path)
	}
}

func TestIndexServesThePage(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<main id="app">`, `/assets/app.js`, `/assets/preview.css`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("want the page to reference %q, got %d %q", want, resp.StatusCode, body)
		}
	}
}

// The vendored libraries must come out with types a browser accepts — a
// module script refused on the wrong one breaks the page — and cache hard,
// since they are baked into the binary.
func TestVendoredAssetsAreServed(t *testing.T) {
	_, ts, _ := newTestServer(t)

	tests := []struct {
		path        string
		status      int
		contentType string
	}{
		{"/assets/css/katex.css", http.StatusOK, "text/css; charset=utf-8"},
		{"/assets/lib/markdown-it.min.js", http.StatusOK, "text/javascript; charset=utf-8"},
		{"/assets/preview.css", http.StatusOK, "text/css; charset=utf-8"},
		{"/assets/app.js", http.StatusOK, "text/javascript; charset=utf-8"},
		{"/assets/fonts/KaTeX_Main-Regular.woff2", http.StatusOK, "font/woff2"},
		{"/assets/ASSETS.md", http.StatusNotFound, ""},
		{"/assets/lib", http.StatusNotFound, ""},
	}
	for _, test := range tests {
		resp, err := http.Get(ts.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != test.status {
			t.Errorf("%s: want %d, got %d", test.path, test.status, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); test.contentType != "" && ct != test.contentType {
			t.Errorf("%s: want Content-Type %q, got %q", test.path, test.contentType, ct)
		}
		if cc := resp.Header.Get("Cache-Control"); test.status == http.StatusOK &&
			cc != "max-age=31536000, immutable" {
			t.Errorf("%s: want immutable caching, got %q", test.path, cc)
		}
	}
}

// sseStream reads events from /events. Everything runs under a deadline, so a
// change that never arrives fails the test instead of hanging it.
type sseStream struct {
	rd *bufio.Reader
}

func openEvents(t *testing.T, baseURL string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return &sseStream{rd: bufio.NewReader(resp.Body)}
}

// readEvent reads one event: an "event:" line, a "data:" line, a blank line.
func (s *sseStream) readEvent(t *testing.T) (string, []byte) {
	t.Helper()
	var name string
	var data []byte
	for {
		line, err := s.rd.ReadString('\n')
		if err != nil {
			t.Fatalf("reading events: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = []byte(strings.TrimPrefix(line, "data: "))
		case line == "":
			if name != "" || data != nil {
				return name, data
			}
		}
	}
}

// A fresh connection receives the current state before anything else — the
// page must not start blank while it waits for the first edit.
func TestEventsStartWithCurrentState(t *testing.T) {
	_, ts, _ := newTestServer(t)

	stream := openEvents(t, ts.URL)
	name, data := stream.readEvent(t)
	if name != "content" {
		t.Fatalf("first event must be content, got %s", name)
	}
	var c struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &c); err != nil || c.Text != fixture {
		t.Errorf("content event carries the document: %q, %v", c.Text, err)
	}

	name, data = stream.readEvent(t)
	if name != "threads" {
		t.Fatalf("second event must be threads, got %s", name)
	}
	var p threadsPayload
	if err := json.Unmarshal(data, &p); err != nil || len(p.Threads) != 0 {
		t.Errorf("threads event carries the review: %q, %v", data, err)
	}
}

// Both watched files drive the stream: editing the document re-renders the
// page, a sidecar another tool wrote updates the review.
func TestEventsDeliverFileChanges(t *testing.T) {
	server, ts, document := newTestServer(t)
	go server.watch(t.Context())

	stream := openEvents(t, ts.URL)
	stream.readEvent(t) // content
	stream.readEvent(t) // threads

	// Shorter text: the size moves even where the filesystem timestamps
	// coarsely.
	if err := os.WriteFile(document, []byte("# Heading\n\nReplaced entirely.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, data := stream.readEvent(t)
	if name != "content" {
		t.Fatalf("an edited document must broadcast content, got %s", name)
	}
	var c struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &c); err != nil || c.Text != "# Heading\n\nReplaced entirely.\n" {
		t.Errorf("content event carries the new text: %q, %v", c.Text, err)
	}

	seedReview(t, document, seededReview)
	name, data = stream.readEvent(t)
	if name != "threads" {
		t.Fatalf("a changed sidecar must broadcast threads, got %s", name)
	}
	var p threadsPayload
	if err := json.Unmarshal(data, &p); err != nil || len(p.Threads) != 2 {
		t.Errorf("threads event carries the review: %q, %v", data, err)
	}
	if len(p.Threads) == 2 && p.Threads[0].ID != "aaa1" {
		t.Errorf("want the thread with its id, got %q", p.Threads[0].ID)
	}
}
