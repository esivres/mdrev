package main

import "github.com/esivres/mdrev/internal/mrsf"

// jsonComment is the machine-readable shape of a comment. It is declared here
// rather than serialised from mrsf.Comment because that struct carries yaml
// tags only, which would expose Go field names and hide the suggested text
// inside a generic extras map — this is an interface agents depend on, so it
// is written out explicitly and follows the field names of the MRSF file.
type jsonComment struct {
	ID           string        `json:"id"`
	Author       string        `json:"author,omitempty"`
	Timestamp    string        `json:"timestamp,omitempty"`
	Text         string        `json:"text"`
	Resolved     bool          `json:"resolved"`
	Line         int           `json:"line,omitempty"`
	Type         string        `json:"type,omitempty"`
	Severity     string        `json:"severity,omitempty"`
	SelectedText string        `json:"selected_text,omitempty"`
	Suggested    string        `json:"x_suggested_text,omitempty"`
	Outcome      string        `json:"x_outcome,omitempty"`
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
		Type:         c.Type,
		Severity:     c.Severity,
		SelectedText: c.SelectedText,
	}
	if s, ok := c.SuggestedText(); ok {
		out.Suggested = s
	}
	out.Outcome = c.Outcome()
	for _, r := range replies {
		out.Replies = append(out.Replies, toJSON(r, nil))
	}
	return out
}
