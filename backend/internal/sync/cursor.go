package sync

import (
	"encoding/base64"
	"strconv"
)

// Cursors are opaque to the client per the API contract ("next opaque
// cursor"), so clients cannot infer or forge ordering from them; internally
// they are just the last sync_changes.cursor value, base64-encoded.

func encodeCursor(n int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(n, 10)))
}

// DecodeCursor parses a client-supplied cursor string. An empty string (first
// ever pull) decodes to 0. An invalid cursor is rejected rather than silently
// treated as 0, so a corrupted client cursor cannot skip changes.
func DecodeCursor(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(raw), 10, 64)
}
