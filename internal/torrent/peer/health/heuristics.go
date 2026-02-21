package health

import "time"

type PeerSample struct {
	Key     string
	RateBps float64
	Uptime  time.Duration
}

// BelowRelativeMean returns peer keys whose rate is below factor * mean rate
// among mature peers. Peers younger than minUptime are ignored.
func BelowRelativeMean(samples []PeerSample, minUptime time.Duration, factor float64) []string {
	if factor <= 0 {
		return nil
	}

	mature := make([]PeerSample, 0, len(samples))
	var sum float64
	for _, s := range samples {
		if s.Key == "" || s.Uptime < minUptime {
			continue
		}
		if s.RateBps < 0 {
			s.RateBps = 0
		}
		mature = append(mature, s)
		sum += s.RateBps
	}
	if len(mature) < 2 {
		return nil
	}

	mean := sum / float64(len(mature))
	cutoff := mean * factor
	if cutoff <= 0 {
		return nil
	}

	out := make([]string, 0, len(mature))
	for _, s := range mature {
		if s.RateBps < cutoff {
			out = append(out, s.Key)
		}
	}
	return out
}
