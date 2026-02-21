package source

import (
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strings"
)

type Kind string

const (
	KindUnknown    Kind = "unknown"
	KindHTTP       Kind = "http"
	KindTorrentURL Kind = "torrent"
	KindMagnet     Kind = "magnet"
)

func Normalize(raw string) string {
	return strings.TrimSpace(raw)
}

func IsHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func IsTorrentURL(raw string) bool {
	if !IsHTTPURL(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".torrent")
}

func IsMagnet(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if strings.ToLower(u.Scheme) != "magnet" {
		return false
	}
	// Accept any non-empty magnet payload (opaque or query).
	return u.Opaque != "" || u.RawQuery != ""
}

func KindOf(raw string) Kind {
	s := Normalize(raw)
	if s == "" {
		return KindUnknown
	}
	if IsMagnet(s) {
		return KindMagnet
	}
	if IsTorrentURL(s) {
		return KindTorrentURL
	}
	if IsHTTPURL(s) {
		return KindHTTP
	}
	return KindUnknown
}

func IsSupported(raw string) bool {
	switch KindOf(raw) {
	case KindHTTP, KindTorrentURL:
		return true
	default:
		return false
	}
}

func CanonicalKey(raw string) (Kind, string) {
	s := Normalize(raw)
	if s == "" {
		return KindUnknown, ""
	}
	if IsMagnet(s) {
		if key := magnetInfoHash(s); key != "" {
			return KindMagnet, key
		}
		// Fallback to normalized magnet string.
		return KindMagnet, strings.ToLower(s)
	}
	if IsHTTPURL(s) {
		if u, err := url.Parse(s); err == nil {
			u.Fragment = ""
			u.Scheme = strings.ToLower(u.Scheme)
			u.Host = strings.ToLower(u.Host)
			return KindOf(s), u.String()
		}
	}
	return KindUnknown, s
}

func magnetInfoHash(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, xt := range q["xt"] {
		// Expected: urn:btih:<hash>
		xt = strings.ToLower(strings.TrimSpace(xt))
		if !strings.HasPrefix(xt, "urn:btih:") {
			continue
		}
		hash := strings.TrimPrefix(xt, "urn:btih:")
		if hash == "" {
			continue
		}
		// Base32 (32 chars) or hex (40 chars) are most common.
		if len(hash) == 40 && isHex(hash) {
			return "btih:" + strings.ToLower(hash)
		}
		if len(hash) == 32 && isBase32(hash) {
			decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(hash))
			if err == nil && len(decoded) == 20 {
				return "btih:" + hex.EncodeToString(decoded)
			}
		}
	}
	return ""
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func isBase32(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '2' && c <= '7':
		default:
			return false
		}
	}
	return true
}

// ParseCommaArg parses a comma-separated input and returns the primary URL and mirrors.
// Mirrors include the primary URL for HTTP/HTTPS inputs (backward compatibility).
func ParseCommaArg(arg string) (string, []string) {
	parts := strings.Split(arg, ",")
	primary := ""
	mirrors := []string{}

	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean == "" {
			continue
		}
		if primary == "" {
			if !IsSupported(clean) {
				continue
			}
			primary = clean
			if IsMagnet(primary) {
				mirrors = append(mirrors, primary)
			} else if IsHTTPURL(primary) {
				mirrors = append(mirrors, primary)
			}
			continue
		}
		// Mirrors are HTTP/HTTPS only.
		if IsHTTPURL(clean) {
			mirrors = append(mirrors, clean)
		}
	}

	if primary == "" {
		return "", nil
	}
	return primary, mirrors
}
