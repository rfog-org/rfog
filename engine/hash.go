package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash returns a stable digest of the state (excluding the log and view-only
// fields). Two states with equal hashes are rules-equivalent.
func Hash(s State) string {
	c := s
	c.Log = nil
	c.Visible = nil
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Fingerprint is a stable digest of the rules content. A client and a
// server with equal fingerprints resolve, preview and validate orders
// identically; the server sends its content to a client whose differs.
func (c *Content) Fingerprint() string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
