package peer

import (
	"testing"
	"time"
)

func TestDialBackoffDuration(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: dialBackoffBase},
		{failures: 1, want: dialBackoffBase},
		{failures: 2, want: 2 * dialBackoffBase},
		{failures: 3, want: 4 * dialBackoffBase},
	}
	for _, tc := range cases {
		if got := dialBackoffDuration(tc.failures); got != tc.want {
			t.Fatalf("failures=%d backoff=%s want=%s", tc.failures, got, tc.want)
		}
	}
	if got := dialBackoffDuration(99); got != dialBackoffMax {
		t.Fatalf("backoff cap mismatch: got=%s want=%s", got, dialBackoffMax)
	}
}

func TestRetryStateBlocksUntilCooldownAndResetsOnSuccess(t *testing.T) {
	m := &Manager{
		retry: make(map[string]dialRetryState),
	}
	const key = "1.2.3.4:6881"
	now := time.Unix(1000, 0)

	if !m.shouldAttemptDialLocked(key, now) {
		t.Fatalf("expected initial dial to be allowed")
	}

	m.noteDialFailureLocked(key, now)
	state := m.retry[key]
	if state.failures != 1 {
		t.Fatalf("failures mismatch: got=%d want=1", state.failures)
	}
	if !state.nextAttempt.Equal(now.Add(dialBackoffBase)) {
		t.Fatalf("nextAttempt mismatch: got=%s want=%s", state.nextAttempt, now.Add(dialBackoffBase))
	}

	if m.shouldAttemptDialLocked(key, now.Add(dialBackoffBase-time.Millisecond)) {
		t.Fatalf("dial should be blocked before cooldown expires")
	}
	if !m.shouldAttemptDialLocked(key, now.Add(dialBackoffBase)) {
		t.Fatalf("dial should be allowed once cooldown expires")
	}

	m.noteDialSuccessLocked(key)
	if !m.shouldAttemptDialLocked(key, now) {
		t.Fatalf("dial should be allowed after success reset")
	}
}

func TestRetryStateEscalatesConsecutiveFailures(t *testing.T) {
	m := &Manager{
		retry: make(map[string]dialRetryState),
	}
	const key = "5.6.7.8:51413"
	now := time.Unix(2000, 0)

	m.noteDialFailureLocked(key, now)
	m.noteDialFailureLocked(key, now)
	m.noteDialFailureLocked(key, now)

	state := m.retry[key]
	if state.failures != 3 {
		t.Fatalf("failures mismatch: got=%d want=3", state.failures)
	}
	if !state.nextAttempt.Equal(now.Add(4 * dialBackoffBase)) {
		t.Fatalf("nextAttempt mismatch: got=%s want=%s", state.nextAttempt, now.Add(4*dialBackoffBase))
	}
}

func TestHealthEvictionBlocksImmediateRedial(t *testing.T) {
	m := &Manager{
		retry: make(map[string]dialRetryState),
	}
	const key = "7.7.7.7:51413"
	now := time.Unix(3000, 0)

	m.noteHealthEvictionLocked(key, now)
	st := m.retry[key]
	if !st.nextAttempt.Equal(now.Add(healthRedialBlock)) {
		t.Fatalf("health block mismatch: got=%s want=%s", st.nextAttempt, now.Add(healthRedialBlock))
	}

	if m.shouldAttemptDialLocked(key, now.Add(healthRedialBlock-time.Millisecond)) {
		t.Fatalf("expected redial to be blocked before health cooldown")
	}
	if !m.shouldAttemptDialLocked(key, now.Add(healthRedialBlock)) {
		t.Fatalf("expected redial to be allowed after health cooldown")
	}
}

func TestCollectHealthEvictionsLockedOnlyWhenSaturated(t *testing.T) {
	now := time.Now()
	mature := minEvictionUptime + 2*time.Second
	makeConn := func(rateBps int64) *Conn {
		return &Conn{
			startedAt: now.Add(-mature),
			received:  rateBps * int64(mature.Seconds()),
		}
	}

	m := &Manager{
		maxPeers: 4,
		active: map[string]*Conn{
			"fast": makeConn(2 * 1024 * 1024),
			"mid":  makeConn(1024 * 1024),
			"slow": makeConn(64 * 1024),
		},
	}

	if victims := m.collectHealthEvictionsLocked(); len(victims) != 0 {
		t.Fatalf("expected no victims when not saturated, got=%v", victims)
	}

	m.maxPeers = 3
	if victims := m.collectHealthEvictionsLocked(); len(victims) == 0 {
		t.Fatalf("expected victims when saturated")
	}
}
