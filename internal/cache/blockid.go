package cache

import (
	"encoding/base64"
	"encoding/binary"
)

// chunkIndexFromBlockID decodes the Azure Blob block id the runner sends on each
// chunk PUT into a 0-based chunk index. Two formats exist (ported from
// falcondev-oss/github-actions-cache-server):
//
//   - 64 bytes: used by tonistiigi/go-actions-cache (Docker Buildx). The chunk
//     index is a big-endian uint32 at byte offset 16.
//   - 48 bytes: used by @actions/cache. The decoded bytes are a 36-char UUID
//     followed by the decimal index.
//
// Returns ok=false if the block id is malformed.
func chunkIndexFromBlockID(blockIDBase64 string) (int, bool) {
	decoded := decodeBase64(blockIDBase64)
	switch len(decoded) {
	case 64:
		return int(binary.BigEndian.Uint32(decoded[16:20])), true
	case 48:
		s := string(decoded)
		if len(s) < 36 {
			return 0, false
		}
		return parseLeadingInt(s[36:])
	default:
		return 0, false
	}
}

// decodeBase64 tries standard and raw/url base64 variants (the runner uses
// standard, but be lenient).
func decodeBase64(s string) []byte {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	return nil
}

// parseLeadingInt parses the leading run of ASCII digits, matching JavaScript's
// Number.parseInt semantics (trailing non-digits are ignored).
func parseLeadingInt(s string) (int, bool) {
	n := 0
	count := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		count++
	}
	if count == 0 {
		return 0, false
	}
	return n, true
}
