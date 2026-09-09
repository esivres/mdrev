package mrsf

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"
)

// LoadOrCreate returns the document's sidecar, creating an empty one in memory
// when the document has no review yet. Nothing touches disk until Save.
func LoadOrCreate(document string) (*Sidecar, error) {
	sc, err := Load(document)
	if err != nil {
		return nil, err
	}
	if sc != nil {
		return sc, nil
	}
	return &Sidecar{
		Version:  "1.0",
		Document: filepath.Base(document),
		path:     Path(document),
	}, nil
}

// Add appends a comment, filling in the fields the spec derives rather than
// asks for: id, timestamp and the anchor hash.
func (s *Sidecar) Add(c Comment) (*Comment, error) {
	if c.ID == "" {
		id, err := uuid4()
		if err != nil {
			return nil, err
		}
		c.ID = id
	}
	if c.Timestamp == "" {
		c.Timestamp = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if c.SelectedText != "" && c.SelectedTextHash == "" {
		sum := sha256.Sum256([]byte(c.SelectedText))
		c.SelectedTextHash = hex.EncodeToString(sum[:])
	}
	s.Comments = append(s.Comments, c)
	return &s.Comments[len(s.Comments)-1], nil
}

func uuid4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
