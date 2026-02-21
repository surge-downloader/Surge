package tracker

import (
	"context"
	"net"
	"testing"
)

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want FailureKind
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: FailureTimeout},
		{name: "dns", err: &net.DNSError{Err: "no such host"}, want: FailureDNS},
		{name: "unknown", err: context.Canceled, want: FailureUnknown},
	}

	for _, tc := range cases {
		got := ClassifyFailure(tc.err)
		if got != tc.want {
			t.Fatalf("%s: got=%v want=%v", tc.name, got, tc.want)
		}
	}
}
