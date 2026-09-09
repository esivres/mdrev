// Package lsp surfaces review comments as editor diagnostics, with code
// actions to apply a suggestion, close a thread, or file a typed-in comment.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
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
	Data        *actionData    `json:"data,omitempty"`
}

// actionData carries what an action needs to do its work when the client comes
// back to resolve it.
type actionData struct {
	Kind    string `json:"kind"` // "apply" or "file"
	URI     string `json:"uri"`
	ID      string `json:"id,omitempty"`
	Anchor  string `json:"anchor,omitempty"`
	Text    string `json:"text,omitempty"`
	Line    int    `json:"line,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	NewText string `json:"newText,omitempty"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Raw rather than any: a response must carry exactly one of result or
	// error, so a nil result still has to be written as null.
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
	// Zed shows nothing from a server's stderr.
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
	_, _ = fmt.Fprintf(s.log, time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
}

func (s *Server) Run(r io.Reader) error {
	// The watcher writes to the same stream, and may be midway through a
	// message, so signalling it is not enough.
	defer func() {
		close(s.done)
		<-s.stopped
		s.outMu.Lock()
		defer s.outMu.Unlock()
		if err := s.out.Flush(); err != nil {
			s.tracef("final flush: %v", err)
		}
	}()
	s.tracef("started pid=%d", os.Getpid())
	go s.watchSidecars()

	br := bufio.NewReader(r)
	for {
		msg, err := readMessage(br)
		if errors.Is(err, io.EOF) {
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
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &length); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q: %w", v, err)
			}
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
	if _, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		s.tracef("write header: %v", err)
		return
	}
	if _, err := s.out.Write(body); err != nil {
		s.tracef("write body: %v", err)
		return
	}
	if err := s.out.Flush(); err != nil {
		s.tracef("flush: %v", err)
	}
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

// A malformed message would otherwise be acted on with zero values — a
// document stored under an empty URI, say — leaving no trace.
func (s *Server) decode(msg *rpcMessage, target any) bool {
	if err := json.Unmarshal(msg.Params, target); err != nil {
		s.tracef("%s: %v", msg.Method, err)
		return false
	}
	return true
}

func (s *Server) handle(msg *rpcMessage) {
	switch msg.Method {
	case "initialize":
		s.reply(msg.ID, map[string]any{
			"capabilities": map[string]any{
				// Spelled out rather than the deprecated number form, so
				// clients actually send didSave.
				"textDocumentSync": map[string]any{
					"openClose": true,
					"change":    1, // full text
					"save":      map[string]any{"includeText": false},
				},
				// Zed applies an action's edit and returns without running its
				// command, so an action that must do both is offered without an
				// edit and resolved on demand instead.
				"codeActionProvider": map[string]any{"resolveProvider": true},
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
		if !s.decode(msg, &p) {
			return
		}
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
		if !s.decode(msg, &p) {
			return
		}
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
		if !s.decode(msg, &p) {
			return
		}
		s.publish(p.TextDocument.URI)
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if !s.decode(msg, &p) {
			return
		}
		s.mu.Lock()
		delete(s.docs, p.TextDocument.URI)
		delete(s.sidecarAt, p.TextDocument.URI)
		s.mu.Unlock()
	case "textDocument/codeAction":
		s.reply(msg.ID, s.codeActions(msg.Params))
	case "codeAction/resolve":
		s.reply(msg.ID, s.resolveCodeAction(msg.Params))
	case "workspace/executeCommand":
		s.reply(msg.ID, s.executeCommand(msg.Params))
	default:
		// A null answer makes clients expecting a list report a decode error.
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
		// Otherwise a broken file looks like a review with nothing in it.
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

// Surfaces comments typed into the document, so the reader can see which have
// not moved into the review yet.
func draftDiagnostics(li *lineIndex) []Diagnostic {
	out := []Diagnostic{}
	for _, d := range findDrafts(li.text) {
		out = append(out, Diagnostic{
			Range:    Range{Start: li.position(d.Start), End: li.position(d.End)},
			Severity: severityInfo,
			Source:   "review-draft",
			Message:  "Not in the review yet: " + summary(d.Text),
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

// Republishes when a sidecar changes on disk, so comments an agent adds from
// the CLI appear without touching the document.
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
	if err := json.Unmarshal(params, &p); err != nil {
		s.tracef("codeAction: %v", err)
		return []CodeAction{}
	}

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
			start, end, found := locateOffsets(li, c)
			if found {
				actions = append(actions, CodeAction{
					Title: "Apply suggestion: " + summary(suggested),
					Kind:  "quickfix",
					Data: &actionData{
						Kind: "apply", URI: uri, ID: c.ID, NewText: suggested,
						Start: start, End: end,
					},
				})
			}
		}
		// Turning down a proposal is a different decision from closing a
		// remark you have dealt with.
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

// The editor asks for actions at the cursor, an empty range, so touching at a
// boundary counts.
func overlaps(a, b Range) bool {
	return !before(a.End, b.Start) && !before(b.End, a.Start)
}

func before(p, q Position) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Character < q.Character)
}

// The edit removes the marker; the command records the comment.
func fileDraftAction(uri string, li *lineIndex, dr draft) CodeAction {
	// Swallow one leading space, or an inline marker leaves a double space.
	from := dr.Start
	if from > 0 && li.text[from-1] == ' ' {
		from--
	}

	return CodeAction{
		Title: "Move into the review: " + summary(dr.Text),
		Kind:  "quickfix",
		Data: &actionData{
			Kind: "file", URI: uri, Anchor: dr.Anchor, Text: dr.Text,
			Line: li.position(dr.Start).Line + 1, Start: from, End: dr.End,
		},
	}
}

// resolveCodeAction fills in the edit the client asked for, and performs the
// write that goes with it. Both have to happen here: Zed applies an edit and
// never runs the action's command, so an action carrying both would silently
// do half its work.
func (s *Server) resolveCodeAction(params json.RawMessage) any {
	var action CodeAction
	if err := json.Unmarshal(params, &action); err != nil {
		s.tracef("resolve action: %v", err)
		return action
	}
	if action.Data == nil {
		return action
	}
	d := action.Data
	path := uriToPath(d.URI)

	s.mu.Lock()
	text, open := s.docs[d.URI]
	s.mu.Unlock()
	if !open {
		return action
	}
	li := newLineIndex(text)

	var err error
	switch d.Kind {
	case "apply":
		err = mrsf.Update(path, func(sc *mrsf.Sidecar) error {
			if c := sc.Find(d.ID); c != nil {
				c.Resolved = true
				c.SetOutcome(mrsf.OutcomeApplied)
			}
			return nil
		})
	case "file":
		err = mrsf.Update(path, func(sc *mrsf.Sidecar) error {
			_, addErr := sc.Add(mrsf.Comment{
				Author:       mrsf.DefaultAuthor(),
				Text:         d.Text,
				Line:         d.Line,
				SelectedText: d.Anchor,
			})
			return addErr
		})
	default:
		return action
	}
	if err != nil {
		// Returning no edit leaves the document as it is, so the comment is
		// still on screen rather than deleted along with the failure.
		s.tracef("resolve %s: %v", d.Kind, err)
		return action
	}

	action.Edit = &WorkspaceEdit{Changes: map[string][]TextEdit{
		d.URI: {{
			Range:   Range{Start: li.position(d.Start), End: li.position(d.End)},
			NewText: d.NewText,
		}},
	}}
	s.publish(d.URI)
	return action
}

// summary labels a menu entry.
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
	if err := json.Unmarshal(params, &p); err != nil {
		s.tracef("executeCommand: %v", err)
		return nil
	}
	switch {
	case p.Command == "mdrev.file" && len(p.Arguments) >= 4:
		return s.fileDraft(p.Arguments[0], p.Arguments[1], p.Arguments[2], p.Arguments[3])
	case p.Command != "mdrev.resolve" || len(p.Arguments) < 3:
		return nil
	}
	uri, id, outcome := p.Arguments[0], p.Arguments[1], p.Arguments[2]

	if err := mrsf.Update(uriToPath(uri), func(sc *mrsf.Sidecar) error {
		c := sc.Find(id)
		if c == nil {
			return nil
		}
		c.Resolved = true
		c.SetOutcome(outcome)
		return nil
	}); err != nil {
		s.tracef("resolve: %v", err)
		return nil
	}

	s.publish(uri)
	return nil
}

// fileDraft records a comment typed into the document.
func (s *Server) fileDraft(uri, anchor, text, line string) any {
	n, _ := strconv.Atoi(line)
	if err := mrsf.Update(uriToPath(uri), func(sc *mrsf.Sidecar) error {
		_, err := sc.Add(mrsf.Comment{
			Author:       mrsf.DefaultAuthor(),
			Text:         text,
			Line:         n,
			SelectedText: anchor,
		})
		return err
	}); err != nil {
		s.tracef("file: %v", err)
		return nil
	}

	s.publish(uri)
	return nil
}
