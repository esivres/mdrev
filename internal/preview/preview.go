// Package preview serves a localhost HTML page that shows a markdown document
// and its review from the MRSF sidecar, and accepts comments, replies and
// resolves over JSON — the same semantics as the comment, reply and resolve
// subcommands.
package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/esivres/mdrev/internal/mrsf"
)

// stamp is the file identity the watcher compares: mtime and size, the same
// pair the LSP server polls sidecars with. Absence is a state of its own, so
// creating or deleting the sidecar counts as a change like any other.
type stamp struct {
	mtime   time.Time
	size    int64
	missing bool
}

func stampOf(path string) stamp {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{missing: true}
	}
	return stamp{mtime: info.ModTime(), size: info.Size()}
}

func sameStamp(a, b stamp) bool {
	return a.missing == b.missing && a.size == b.size && a.mtime.Equal(b.mtime)
}

// sseEvent is one server-sent event, its payload already encoded.
type sseEvent struct {
	name string
	data []byte
}

// Server previews one document.
type Server struct {
	document string
	handler  http.Handler

	// mu guards everything below. A broadcast updates the snapshot and
	// delivers to subscribers under this lock, and so does subscribing, so a
	// client is seeded with one consistent state and receives every change
	// after it — never older data once newer has been seen.
	mu   sync.Mutex
	subs map[chan sseEvent]struct{}
	// Snapshots of the last successfully read state, encoded as SSE payloads.
	// Nil until the first successful read; subscribe falls back to reading
	// the files itself.
	content []byte // {"text":...}
	threads []byte // {"threads":[...]}, resolved included

	docStamp     stamp
	sidecarStamp stamp
}

// New prepares a server for one document. The path is made absolute so the
// page, the sidecar and the watcher describe the same file even if the
// process changes directory later.
func New(document string) (*Server, error) {
	document, err := filepath.Abs(document)
	if err != nil {
		return nil, err
	}
	s := &Server{
		document: document,
		subs:     make(map[chan sseEvent]struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.serveIndex)
	mux.HandleFunc("GET /events", s.serveEvents)
	mux.HandleFunc("GET /api/content", s.serveContent)
	mux.HandleFunc("GET /api/threads", s.serveThreads)
	mux.HandleFunc("GET /api/author", s.serveAuthor)
	mux.HandleFunc("POST /api/comment", s.serveComment)
	mux.HandleFunc("POST /api/reply", s.serveReply)
	mux.HandleFunc("POST /api/resolve", s.serveResolve)
	mux.Handle("GET /assets/", http.HandlerFunc(s.serveAssets))
	s.handler = mux
	return s, nil
}

// Handler serves the preview. Watching starts with Run; a handler alone is
// still usable for the request/response endpoints.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run serves until the listener is closed or ctx is cancelled — an interrupt,
// in the CLI — and runs the 1-second watcher alongside.
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	go s.watch(ctx)
	serveErr := make(chan error, 1)
	go func() { serveErr <- http.Serve(ln, s.handler) }()

	select {
	case <-ctx.Done():
		_ = ln.Close()
		<-serveErr
		return nil
	case err := <-serveErr:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// watch broadcasts document and sidecar changes to connected clients. A second
// of latency is the price of not depending on platform file notifications —
// the same trade the LSP server makes for sidecars.
func (s *Server) watch(ctx context.Context) {
	// A client receives the current state on connect, so priming the stamps
	// keeps the startup state from firing as a change.
	s.mu.Lock()
	s.docStamp = stampOf(s.document)
	s.sidecarStamp = stampOf(mrsf.Path(s.document))
	s.mu.Unlock()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.poll()
	}
}

// poll compares the stamps and refreshes what moved. A stat or read error
// skips the cycle rather than failing it: a save by another tool is
// momentarily unreadable, and the stamp is only moved forward on a complete
// refresh, so the next tick retries.
func (s *Server) poll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if at := stampOf(s.document); !sameStamp(at, s.docStamp) {
		if s.setContent() {
			s.docStamp = at
		}
	}
	if at := stampOf(mrsf.Path(s.document)); !sameStamp(at, s.sidecarStamp) {
		if s.setThreads() {
			s.sidecarStamp = at
		}
	}
}

// setContent refreshes the document snapshot and tells every subscriber.
// Called with s.mu held.
func (s *Server) setContent() bool {
	data, err := os.ReadFile(s.document)
	if err != nil {
		return false
	}
	payload, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: string(data)})
	if err != nil {
		return false
	}
	s.content = payload
	s.broadcast(sseEvent{name: "content", data: payload})
	return true
}

// setThreads refreshes the thread snapshot and tells every subscriber.
// Resolved threads stay in: a thread another participant closes must arrive
// as closed, not vanish. Called with s.mu held.
func (s *Server) setThreads() bool {
	sc, err := mrsf.Load(s.document)
	if err != nil {
		return false
	}
	if sc == nil {
		sc = &mrsf.Sidecar{}
	}
	payload, err := json.Marshal(threadsPayload{Threads: threadList(sc, true)})
	if err != nil {
		return false
	}
	s.threads = payload
	s.broadcast(sseEvent{name: "threads", data: payload})
	return true
}

// broadcast delivers an event to every subscriber. A stalled tab misses the
// event rather than stalling the writer; its next change resynchronises it.
// Called with s.mu held.
func (s *Server) broadcast(ev sseEvent) {
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// subscribe registers a client, seeding its channel with the current state:
// content and threads arrive before anything else. Seeding under the lock
// keeps them ahead of every later broadcast.
func (s *Server) subscribe() chan sseEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.content == nil {
		s.setContent()
	}
	if s.threads == nil {
		s.setThreads()
	}
	ch := make(chan sseEvent, 8)
	if s.content != nil {
		ch <- sseEvent{name: "content", data: s.content}
	}
	if s.threads != nil {
		ch <- sseEvent{name: "threads", data: s.threads}
	}
	s.subs[ch] = struct{}{}
	return ch
}

func (s *Server) unsubscribe(ch chan sseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, ch)
}

// serveEvents streams changes as server-sent events until the client hangs up.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := s.subscribe()
	defer s.unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	// The headers must reach the client now, not when the buffer fills.
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			// Payloads are single-line JSON, so one event is exactly these
			// three lines.
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
