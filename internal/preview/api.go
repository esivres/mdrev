package preview

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/esivres/mdrev/internal/anchor"
	"github.com/esivres/mdrev/internal/mrsf"
)

// jsonComment is the machine-readable shape of a comment — a replica of
// cmd/mdrev/json.go, which cannot be imported from here (package main), plus
// end_line. Keep the two in sync.
type jsonComment struct {
	ID           string        `json:"id"`
	Author       string        `json:"author,omitempty"`
	Timestamp    string        `json:"timestamp,omitempty"`
	Text         string        `json:"text"`
	Resolved     bool          `json:"resolved"`
	Line         int           `json:"line,omitempty"`
	EndLine      int           `json:"end_line,omitempty"`
	Type         string        `json:"type,omitempty"`
	Severity     string        `json:"severity,omitempty"`
	SelectedText string        `json:"selected_text,omitempty"`
	Suggested    string        `json:"x_suggested_text,omitempty"`
	Outcome      string        `json:"x_outcome,omitempty"`
	Orphaned     bool          `json:"orphaned,omitempty"`
	Replies      []jsonComment `json:"replies,omitempty"`
}

func toJSON(c mrsf.Comment, replies []mrsf.Comment) jsonComment {
	out := jsonComment{
		ID:           c.ID,
		Author:       c.Author,
		Timestamp:    c.Timestamp,
		Text:         c.Text,
		Resolved:     c.Resolved,
		Line:         c.Line,
		EndLine:      c.EndLine,
		Type:         c.Type,
		Severity:     c.Severity,
		SelectedText: c.SelectedText,
	}
	if s, ok := c.SuggestedText(); ok {
		out.Suggested = s
	}
	out.Outcome = c.Outcome()
	out.Orphaned = c.Orphaned()
	for _, r := range replies {
		out.Replies = append(out.Replies, toJSON(r, nil))
	}
	return out
}

type threadsPayload struct {
	Threads []jsonComment `json:"threads"`
}

// threadList encodes threads the way `mdrev list --json` prints them: an
// array of top-level comments carrying their replies. Empty is [] rather than
// null — a page parsing this should see an empty list, not absence.
func threadList(sc *mrsf.Sidecar, includeResolved bool) []jsonComment {
	out := []jsonComment{}
	for _, t := range sc.Threads(includeResolved) {
		out = append(out, toJSON(t.Parent, t.Replies))
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload) // a vanished client cannot be helped
}

// fail answers a rejected request: {"ok":false,"error":...}.
func fail(w http.ResponseWriter, code int, format string, args ...any) {
	writeJSON(w, code, struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}{Error: fmt.Sprintf(format, args...)})
}

// badRequest marks a client mistake, so a handler answers 4xx rather than 500.
type badRequest struct{ err error }

func (b badRequest) Error() string { return b.err.Error() }
func (b badRequest) Unwrap() error { return b.err }

// reportMutationError distinguishes a rejected request from a failure to save.
func reportMutationError(w http.ResponseWriter, err error) {
	var bad badRequest
	if errors.As(err, &bad) {
		fail(w, http.StatusBadRequest, "%s", err)
		return
	}
	fail(w, http.StatusInternalServerError, "%s", err)
}

// decode reads a JSON body, capped: a runaway client must not buffer the
// server to death.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad request body: %v", err)
		return false
	}
	return true
}

// serveContent returns the document as it is on disk right now.
func (s *Server) serveContent(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(s.document)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%s", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}{Path: s.document, Text: string(data)})
}

// serveThreads lists the review; ?all=1 adds resolved threads, like
// `mdrev list --all`. Read fresh each time: another tool may have moved the
// sidecar between requests.
func (s *Server) serveThreads(w http.ResponseWriter, r *http.Request) {
	sc, err := mrsf.Load(s.document)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%s", err)
		return
	}
	if sc == nil {
		sc = &mrsf.Sidecar{}
	}
	writeJSON(w, http.StatusOK, threadsPayload{
		Threads: threadList(sc, r.URL.Query().Get("all") != ""),
	})
}

// commentRequest is the body of POST /api/comment.
type commentRequest struct {
	Text   string `json:"text"`
	Type   string `json:"type"`
	Quote  string `json:"quote"`
	Line   int    `json:"line"`
	Author string `json:"author"`
}

var commentTypes = map[string]bool{"": true, "issue": true, "question": true, "suggestion": true}

// serveComment records a comment with the semantics of `mdrev comment`.
func (s *Server) serveComment(w http.ResponseWriter, r *http.Request) {
	var req commentRequest
	if !decode(w, r, &req) {
		return
	}
	// A form paste can carry a trailing newline; whitespace is never content.
	req.Text = strings.TrimSpace(req.Text)
	req.Quote = strings.TrimSpace(req.Quote)
	if req.Text == "" {
		fail(w, http.StatusBadRequest, "comment text is empty")
		return
	}
	if !commentTypes[req.Type] {
		fail(w, http.StatusBadRequest, "unknown type %q; want issue, question or suggestion", req.Type)
		return
	}
	if req.Line < 0 {
		fail(w, http.StatusBadRequest, "line is 1-based, got %d", req.Line)
		return
	}

	added, err := s.addComment(req)
	if err != nil {
		reportMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK      bool        `json:"ok"`
		Comment jsonComment `json:"comment"`
	}{OK: true, Comment: added})
}

// addComment appends through a fresh sidecar load, the way the CLI does: the
// sidecar may have moved since the page last looked.
func (s *Server) addComment(req commentRequest) (jsonComment, error) {
	if req.Quote == "" && req.Line > 0 {
		req.Quote = lineWords(s.document, req.Line)
	}
	c := mrsf.Comment{
		Author:       firstNonEmpty(req.Author, mrsf.DefaultAuthor()),
		Text:         req.Text,
		Line:         req.Line,
		SelectedText: req.Quote,
		Type:         req.Type,
	}
	var added *mrsf.Comment
	err := mrsf.Update(s.document, func(sc *mrsf.Sidecar) error {
		var err error
		added, err = sc.Add(c)
		return err
	})
	if err != nil {
		return jsonComment{}, err
	}
	return toJSON(*added, nil), nil
}

// serveAuthor tells the page which name comments get when the reviewer
// leaves the field empty, so the compose form can prefill it right away.
func (s *Server) serveAuthor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Author string `json:"author"`
	}{Author: mrsf.DefaultAuthor()})
}

// replyRequest is the body of POST /api/reply.
type replyRequest struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// serveReply answers a thread, inheriting its anchor the way `mdrev reply`
// does.
func (s *Server) serveReply(w http.ResponseWriter, r *http.Request) {
	var req replyRequest
	if !decode(w, r, &req) {
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		fail(w, http.StatusBadRequest, "reply text is empty")
		return
	}

	var added *mrsf.Comment
	err := mrsf.Update(s.document, func(sc *mrsf.Sidecar) error {
		parent, err := findByPrefix(sc, req.ID)
		if err != nil {
			return err
		}
		added, err = sc.Add(mrsf.Comment{
			Author:       mrsf.DefaultAuthor(),
			Text:         req.Text,
			Line:         parent.Line,
			SelectedText: parent.SelectedText,
			ReplyTo:      parent.ID,
		})
		return err
	})
	if err != nil {
		reportMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK      bool        `json:"ok"`
		Comment jsonComment `json:"comment"`
	}{OK: true, Comment: toJSON(*added, nil)})
}

// resolveRequest is the body of POST /api/resolve.
type resolveRequest struct {
	ID string `json:"id"`
}

// serveResolve closes a thread the way `mdrev resolve` does, recording how it
// ended so an agent cannot mistake settled for dismissed.
func (s *Server) serveResolve(w http.ResponseWriter, r *http.Request) {
	var req resolveRequest
	if !decode(w, r, &req) {
		return
	}
	var closedID string
	err := mrsf.Update(s.document, func(sc *mrsf.Sidecar) error {
		c, err := findByPrefix(sc, req.ID)
		if err != nil {
			return err
		}
		c.Resolved = true
		c.SetOutcome(mrsf.OutcomeResolved)
		closedID = c.ID
		return nil
	})
	if err != nil {
		reportMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}{OK: true, ID: closedID})
}

// findByPrefix resolves an id the way the CLI does: a prefix is enough, but it
// must stay unambiguous (cmd/mdrev/reply.go).
func findByPrefix(s *mrsf.Sidecar, prefix string) (*mrsf.Comment, error) {
	var found *mrsf.Comment
	for i := range s.Comments {
		if strings.HasPrefix(s.Comments[i].ID, prefix) {
			if found != nil {
				return nil, badRequest{fmt.Errorf("id %q is ambiguous", prefix)}
			}
			found = &s.Comments[i]
		}
	}
	if found == nil {
		return nil, badRequest{fmt.Errorf("no comment with id %q", prefix)}
	}
	return found, nil
}

// lineWords quotes the start of a line, for a comment made without selecting
// anything — the same fallback anchor `mdrev comment` uses
// (cmd/mdrev/main.go).
func lineWords(document string, line int) string {
	data, err := os.ReadFile(document)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return anchor.After(lines[line-1])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
