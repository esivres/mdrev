// Package mrsf reads and writes Markdown Review Sidecar Format files
// (https://sidemark.org). Only the subset needed for editor integration is
// modelled; unknown keys survive a load/save round-trip via Extra.
package mrsf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SuggestedTextKey carries a proposed replacement. MRSF has no field for one,
// so it rides in the spec's x_* extension namespace.
const SuggestedTextKey = "x_suggested_text"

type Comment struct {
	ID               string         `yaml:"id"`
	Author           string         `yaml:"author,omitempty"`
	Timestamp        string         `yaml:"timestamp,omitempty"`
	Text             string         `yaml:"text"`
	Resolved         bool           `yaml:"resolved"`
	Line             int            `yaml:"line,omitempty"`
	EndLine          int            `yaml:"end_line,omitempty"`
	StartColumn      *int           `yaml:"start_column,omitempty"`
	EndColumn        *int           `yaml:"end_column,omitempty"`
	Type             string         `yaml:"type,omitempty"`
	Severity         string         `yaml:"severity,omitempty"`
	SelectedText     string         `yaml:"selected_text,omitempty"`
	SelectedTextHash string         `yaml:"selected_text_hash,omitempty"`
	ReplyTo          string         `yaml:"reply_to,omitempty"`
	Extra            map[string]any `yaml:",inline"`
}

func (c Comment) SuggestedText() (string, bool) {
	s, ok := c.Extra[SuggestedTextKey].(string)
	return s, ok && s != ""
}

type Sidecar struct {
	Version  string    `yaml:"mrsf_version"`
	Document string    `yaml:"document"`
	Comments []Comment `yaml:"comments"`
	// Keys other tools wrote; without this, every save would delete them.
	Extra map[string]any `yaml:",inline"`

	path string
}

// Path matches the layout the reference mrsf CLI creates.
func Path(document string) string {
	return document + ".review.yaml"
}

// Load returns nil, nil when there is no sidecar: no review is a normal state.
func Load(document string) (*Sidecar, error) {
	path := Path(document)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Sidecar
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.path = path
	return &s, nil
}

// save replaces the sidecar whole: a half-written file still parses as YAML
// with comments missing, and the next save would make that permanent.
func (s *Sidecar) save() error {
	s.dropShadowedKeys()

	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	// Rename would turn a symlinked sidecar into a regular file.
	target := s.path
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".mrsf-*.yaml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.WriteString(buf.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), s.perm()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	// The rename is durable only once the directory entry is on disk.
	if d, err := os.Open(dir); err == nil {
		defer func() { _ = d.Close() }()
		return d.Sync()
	}
	return nil
}

// The yaml encoder panics on an extra that shadows a modelled field; losing
// the duplicate beats losing the review.
func (s *Sidecar) dropShadowedKeys() {
	delete(s.Extra, "mrsf_version")
	delete(s.Extra, "document")
	delete(s.Extra, "comments")
	for i := range s.Comments {
		for _, key := range []string{
			"id", "author", "timestamp", "text", "resolved", "line", "end_line",
			"start_column", "end_column", "type", "severity", "selected_text",
			"selected_text_hash", "reply_to",
		} {
			delete(s.Comments[i].Extra, key)
		}
	}
}

// Replacing the file must not widen access to a private review.
func (s *Sidecar) perm() os.FileMode {
	if info, err := os.Stat(s.path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}

func (s *Sidecar) Find(id string) *Comment {
	for i := range s.Comments {
		if s.Comments[i].ID == id {
			return &s.Comments[i]
		}
	}
	return nil
}

// How a thread ended. Applying a suggestion and turning it down both close it,
// so without this an agent cannot tell the two apart and proposes again.
const (
	OutcomeKey       = "x_outcome"
	OutcomeApplied   = "applied"
	OutcomeDismissed = "dismissed"
	OutcomeResolved  = "resolved"
)

func (c Comment) Outcome() string {
	s, _ := c.Extra[OutcomeKey].(string)
	return s
}

func (c *Comment) SetOutcome(outcome string) {
	if outcome == "" {
		return
	}
	if c.Extra == nil {
		c.Extra = map[string]any{}
	}
	c.Extra[OutcomeKey] = outcome
}
