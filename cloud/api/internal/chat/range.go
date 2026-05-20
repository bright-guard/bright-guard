package chat

import (
	"strings"
	"time"
)

// parseRange turns a "7d" / "24h" / "30d" style string into (from, to). Anything
// blank or unparseable becomes a 7-day window ending now.
func parseRange(s string) (from, to time.Time) {
	to = time.Now().UTC()
	d := 7 * 24 * time.Hour
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return to.Add(-d), to
	}
	unit := time.Duration(0)
	var num string
	switch {
	case strings.HasSuffix(s, "h"):
		unit = time.Hour
		num = strings.TrimSuffix(s, "h")
	case strings.HasSuffix(s, "d"):
		unit = 24 * time.Hour
		num = strings.TrimSuffix(s, "d")
	default:
		return to.Add(-d), to
	}
	n := 0
	for _, ch := range num {
		if ch < '0' || ch > '9' {
			return to.Add(-d), to
		}
		n = n*10 + int(ch-'0')
		if n > 365 {
			n = 365
		}
	}
	if n <= 0 {
		return to.Add(-d), to
	}
	return to.Add(-time.Duration(n) * unit), to
}
