package audio

import (
	"testing"
	"time"
)

func TestNextWatchDelay(t *testing.T) {
	cases := []struct {
		name    string
		current time.Duration
		healthy bool
		want    time.Duration
	}{
		{"healthy resets", time.Minute, true, healthyWatchDelay},
		{"first failure waits longer", firstWatchDelay, false, healthyWatchDelay},
		{"second failure doubles", healthyWatchDelay, false, 30 * time.Second},
		{"third failure doubles", 30 * time.Second, false, time.Minute},
		{"caps at max", time.Minute, false, maxWatchDelay},
		{"stays capped", maxWatchDelay, false, maxWatchDelay},
	}
	for _, tc := range cases {
		if got := nextWatchDelay(tc.current, tc.healthy); got != tc.want {
			t.Errorf("%s: nextWatchDelay(%s, %v) = %s, want %s", tc.name, tc.current, tc.healthy, got, tc.want)
		}
	}
}
