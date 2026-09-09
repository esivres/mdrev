// Package lsp implements the language server that surfaces MRSF review
// comments as editor diagnostics, with code actions to apply or dismiss
// suggested edits.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/esivres/mdrev/internal/mrsf"
)

// Version is stamped by the build and reported to the editor.
var Version = "dev"

const (
	severityWarn = 2
	severityInfo = 3
	severityHint = 4
)

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *Command       `json:"command,omitempty"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Result is a raw message rather than any: a response must carry exactly
	// one of result or error, and an omitted result is a protocol error even
	// when the request has nothing to return.
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	mu        sync.Mutex
	docs      map[string]string // uri -> text
	sidecarAt map[string]time.Time
	out       *bufio.Writer
	outMu     sync.Mutex
	done      chan struct{}
	stopped   chan struct{}
	log       *os.File
}

func NewServer(out io.Writer) *Server {
	s := &Server{
		docs:      map[string]string{},
		sidecarAt: map[string]time.Time{},
		out:       bufio.NewWriter(out),
		done:      make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	// Zed shows nothing from a language server's stderr, so keep a trace file.
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "mdrev-lsp.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		s.log = f
	}
	return s
}

func (s *Server) tracef(format string, args ...any) {
	if s.log == nil {
		return
	}
	fmt.Fprintf(s.log, time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
}

func (s *Server) Run(r io.Reader) error {
	// The watcher writes to the same stream, so the flush is only final once it
	// has actually stopped — signalling it is not enough, it may be midway
	// through building a message.
	defer func() {
		close(s.done)
		<-s.stopped
		s.outMu.Lock()
		defer s.outMu.Unlock()
		s.out.Flush()
	}()
	s.tracef("started pid=%d", os.Getpid())
	go s.watchSidecars()

	br := bufio.NewReader(r)
	for {
		msg, err := readMessage(br)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.handle(msg)
	}
}

func readMessage(r *bufio.Reader) (*rpcMessage, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			fmt.Sscanf(strings.TrimSpace(v), "%d", &length)
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("message without Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Server) send(msg rpcMessage) {
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		s.tracef("marshal: %v", err)
		return
	}
	s.outMu.Lock()
	defer s.outMu.Unlock()
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(body))
	s.out.Write(body)
	s.out.Flush()
}

func (s *Server) reply(id json.RawMessage, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		s.tracef("marshal result: %v", err)
		raw = []byte("null")
	}
	s.send(rpcMessage{ID: id, Result: raw})
}

func (s *Server) notify(method string, params any) {
	raw, _ := json.Marshal(params)
	s.send(rpcMessage{Method: method, Params: raw})
}

func (s *Server) handle(msg *rpcMessage) {
	switch msg.Method {
	case "initialize":
		s.reply(msg.ID, map[string]any{
			"capabilities": map[string]any{
				// Spelled out rather than the deprecated number form, so that
				// clients actually send didSave, which is when applied
				// suggestions get closed.
				"textDocumentSync": map[string]any{
					"openClose": true,
					"change":    1, // full text
					"save":      map[string]any{"includeText": false},
				},
				"codeActionProvider": true,
				"executeCommandProvider": map[string]any{
					"commands": []string{"mdrev.resolve", "mdrev.file"},
				},
			},
			"serverInfo": map[string]any{"name": "mdrev", "version": Version},
		})
	case "shutdown":
		s.reply(msg.ID, nil)
	case "exit":
		os.Exit(0)
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		json.Unmarshal(msg.Params, &p)
		s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
		s.publish(p.TextDocument.URI)
	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		json.Unmarshal(msg.Params, &p)
		if len(p.ContentChanges) > 0 {
			s.setDoc(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
			s.publish(p.TextDocument.URI)
		}
	case "textDocument/didSave":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(msg.Params, &p)
		s.publish(p.TextDocument.URI)
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(msg.Params, &p)
		s.mu.Lock()
		delete(s.docs, p.TextDocument.URI)
		delete(s.sidecarAt, p.TextDocument.URI)
		s.mu.Unlock()
	case "textDocument/codeAction":
		s.reply(msg.ID, s.codeActions(msg.Params))
	case "workspace/executeCommand":
		s.reply(msg.ID, s.executeCommand(msg.Params))
	default:
		// Answering an unsupported request with null makes clients that expect
		// a list report a decoding error; say "no such method" instead.
		if len(msg.ID) > 0 && string(msg.ID) != "null" {
			s.send(rpcMessage{ID: msg.ID, Error: &rpcError{
				Code:    -32601,
				Message: "method not supported: " + msg.Method,
			}})
		}
	}
}

func (s *Server) setDoc(uri, text string) {
	s.mu.Lock()
	s.docs[uri] = text
	s.mu.Unlock()
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	return u.Path
}

func severityFor(c mrsf.Comment) int {
	if c.Type == "suggestion" {
		return severityHint
	}
	switch c.Severity {
	case "high":
		return severityWarn
	case "low":
		return severityHint
	default:
		return severityInfo
	}
}

func (s *Server) diagnostics(uri string) []Diagnostic {
	s.mu.Lock()
	text, ok := s.docs[uri]
	s.mu.Unlock()
	if !ok {
		return []Diagnostic{}
	}
	sc, err := mrsf.Load(uriToPath(uri))
	if err != nil {
		// Every comment is invisible while the file is unparseable, so say so
		// in the editor instead of looking like a review with nothing in it.
		s.tracef("sidecar: %v", err)
		li := newLineIndex(text)
		return append([]Diagnostic{{
			Range:    Range{},
			Severity: severityWarn,
			Source:   "review",
			Message:  "The review file cannot be read, so no comments are shown: " + err.Error(),
		}}, draftDiagnostics(li)...)
	}
	if sc == nil {
		return draftDiagnostics(newLineIndex(text))
	}

	li := newLineIndex(text)
	out := draftDiagnostics(li)
	for _, t := range sc.Threads(false) {
		c := t.Parent
		rng, anchored := locate(li, c)
		author := c.Author
		if author == "" {
			author = "?"
		}
		msg := author + ": " + c.Text
		if suggested, ok := c.SuggestedText(); ok {
			msg += "\n→ " + suggested
		}
		for _, r := range t.Replies {
			msg += "\n\n" + r.Author + ": " + r.Text
		}
		if !anchored && (c.SelectedText != "" || !li.hasLine(c.Line)) {
			msg = "[anchor lost] " + msg
		}
		out = append(out, Diagnostic{
			Range:    rng,
			Severity: severityFor(c),
			Source:   "review",
			Message:  msg,
		})
	}
	return out
}

// draftDiagnostics surface comments typed into the document as CriticMarkup,
// so the reader can see they are not filed yet.
func draftDiagnostics(li *lineIndex) []Diagnostic {
	out := []Diagnostic{}
	for _, d := range findDrafts(li.text) {
		out = append(out, Diagnostic{
			Range:    Range{Start: li.position(d.Start), End: li.position(d.End)},
			Severity: severityInfo,
			Source:   "review-draft",
			Message:  "Unfiled comment: " + summary(d.Text),
		})
	}
	return out
}

func (s *Server) publish(uri string) {
	s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": s.diagnostics(uri),
	})
}

// watchSidecars republishes when a sidecar changes on disk, so comments an
// agent adds from the CLI appear without touching the document.
func (s *Server) watchSidecars() {
	defer close(s.stopped)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}

		s.mu.Lock()
		uris := make([]string, 0, len(s.docs))
		for uri := range s.docs {
			uris = append(uris, uri)
		}
		s.mu.Unlock()

		for _, uri := range uris {
			info, err := os.Stat(mrsf.Path(uriToPath(uri)))
			if err != nil {
				continue
			}
			s.mu.Lock()
			changed := !s.sidecarAt[uri].Equal(info.ModTime())
			s.sidecarAt[uri] = info.ModTime()
			s.mu.Unlock()
			if changed {
				s.publish(uri)
			}
		}
	}
}

func (s *Server) codeActions(params json.RawMessage) []CodeAction {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range Range `json:"range"`
	}
	json.Unmarshal(params, &p)

	uri := p.TextDocument.URI
	s.mu.Lock()
	text, open := s.docs[uri]
	s.mu.Unlock()
	if !open {
		return []CodeAction{}
	}
	li := newLineIndex(text)

	actions := []CodeAction{}
	for _, d := range findDrafts(text) {
		rng := Range{Start: li.position(d.Start), End: li.position(d.End)}
		if overlaps(rng, p.Range) {
			actions = append(actions, fileDraftAction(uri, li, d))
		}
	}

	sc, err := mrsf.Load(uriToPath(uri))
	if err != nil || sc == nil {
		return actions
	}
	for _, t := range sc.Threads(false) {
		c := t.Parent
		rng, anchored := locate(li, c)
		if !overlaps(rng, p.Range) {
			continue
		}
		suggested, hasSuggestion := c.SuggestedText()
		if hasSuggestion && anchored {
			actions = append(actions, CodeAction{
				Title: "Apply suggestion: " + summary(suggested),
				Kind:  "quickfix",
				Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{
					uri: {{Range: rng, NewText: suggested}},
				}},
				Command: &Command{
					Title:     "resolve",
					Command:   "mdrev.resolve",
					Arguments: []any{uri, c.ID, mrsf.OutcomeApplied},
				},
			})
		}
		// Naming the two cases apart: turning down a proposed edit is a
		// different decision from closing a remark you have dealt with.
		closeTitle := "Resolve comment: " + summary(c.Text)
		if hasSuggestion {
			closeTitle = "Keep the current wording: " + summary(c.SelectedText)
		}
		outcome := mrsf.OutcomeResolved
		if hasSuggestion {
			outcome = mrsf.OutcomeDismissed
		}
		actions = append(actions, CodeAction{
			Title: closeTitle,
			Kind:  "quickfix",
			Command: &Command{
				Title:     "resolve",
				Command:   "mdrev.resolve",
				Arguments: []any{uri, c.ID, outcome},
			},
		})
	}
	return actions
}

// overlaps reports whether two ranges touch. The editor asks for actions at
// the cursor, which is an empty range, so touching at a boundary counts.
func overlaps(a, b Range) bool {
	return !before(a.End, b.Start) && !before(b.End, a.Start)
}

func before(p, q Position) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Character < q.Character)
}

// fileDraftAction turns a CriticMarkup draft into a filed comment: the edit
// removes the marker from the document, the command records it in the sidecar.
func fileDraftAction(uri string, li *lineIndex, dr draft) CodeAction {
	// Swallow one leading space so removing an inline marker does not leave a
	// double space behind.
	from := dr.Start
	if from > 0 && li.text[from-1] == ' ' {
		from--
	}
	line := li.position(dr.Start).Line + 1

	return CodeAction{
		Title: "File as review comment",
		Kind:  "quickfix",
		Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{
			uri: {{Range: Range{Start: li.position(from), End: li.position(dr.End)}, NewText: ""}},
		}},
		Command: &Command{
			Title:     "file",
			Command:   "mdrev.file",
			Arguments: []any{uri, dr.Anchor, dr.Text, strconv.Itoa(line)},
		},
	}
}

// summary is a one-line label for a menu entry.
func summary(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len([]rune(s)) > 50 {
		s = string([]rune(s)[:50]) + "…"
	}
	return s
}

func (s *Server) executeCommand(params json.RawMessage) any {
	var p struct {
		Command   string   `json:"command"`
		Arguments []string `json:"arguments"`
	}
	json.Unmarshal(params, &p)
	switch {
	case p.Command == "mdrev.file" && len(p.Arguments) >= 4:
		return s.fileDraft(p.Arguments[0], p.Arguments[1], p.Arguments[2], p.Arguments[3])
	case p.Command != "mdrev.resolve" || len(p.Arguments) < 3:
		return nil
	}
	uri, id, outcome := p.Arguments[0], p.Arguments[1], p.Arguments[2]

	sc, err := mrsf.Load(uriToPath(uri))
	if err != nil || sc == nil {
		s.tracef("resolve: %v", err)
		return nil
	}
	c := sc.Find(id)
	if c == nil {
		return nil
	}
	c.Resolved = true
	c.SetOutcome(outcome)
	if err := sc.Save(); err != nil {
		s.tracef("save: %v", err)
		return nil
	}
	s.publish(uri)
	return nil
}

// fileDraft records a comment the human typed into the document. The author is
// the human, not the agent, so it is left to the sidecar's default rather than
// guessed here.
func (s *Server) fileDraft(uri, anchor, text, line string) any {
	sidecar, err := mrsf.LoadOrCreate(uriToPath(uri))
	if err != nil {
		s.tracef("file: %v", err)
		return nil
	}
	n, _ := strconv.Atoi(line)
	if _, err := sidecar.Add(mrsf.Comment{
		Author:       mrsf.DefaultAuthor(),
		Text:         text,
		Line:         n,
		SelectedText: anchor,
	}); err != nil {
		s.tracef("file: %v", err)
		return nil
	}
	if err := sidecar.Save(); err != nil {
		s.tracef("save: %v", err)
		return nil
	}
	s.publish(uri)
	return nil
}
