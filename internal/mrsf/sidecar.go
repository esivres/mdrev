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

// SuggestedTextKey holds a proposed replacement for the anchored text. MRSF has
// no field for this, so it rides in the spec's x_* extension namespace.
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
	// Extra keeps top-level keys other tools wrote. Without it every save
	// through this package would quietly delete their metadata.
	Extra map[string]any `yaml:",inline"`

	path string
}

// Path returns the sidecar path for a document, matching the layout the mrsf
// CLI creates: doc.md -> doc.md.review.yaml.
func Path(document string) string {
	return document + ".review.yaml"
}

// Load returns a nil Sidecar (and no error) when the document has no sidecar,
// so callers can treat "no review in progress" as the normal case.
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

// Save writes the sidecar atomically. A review is the only copy of a
// discussion, and a half-written file can still parse as valid YAML with
// comments missing — which the next save would make permanent.
func (s *Sidecar) Save() error {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".mrsf-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(buf.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func (s *Sidecar) Find(id string) *Comment {
	for i := range s.Comments {
		if s.Comments[i].ID == id {
			return &s.Comments[i]
		}
	}
	return nil
}
