package core

import (
	"context"
	"testing"
	"time"
)

func TestSleepReturnsFalseWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Second) {
		t.Fatal("a cancelled context must not report a full wait")
	}
}

func TestSleepReturnsTrueWhenItElapses(t *testing.T) {
	if !Sleep(context.Background(), time.Millisecond) {
		t.Fatal("an uncancelled wait must report true")
	}
}

func TestHashTokenIsTheDigestHex(t *testing.T) {
	got := HashToken("abc")
	if len(got) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(got))
	}
	if got == HashToken("abd") {
		t.Fatal("different tokens must not collide")
	}
}

func TestRandomHexReturnsEntropy(t *testing.T) {
	a, err := RandomHex(16)
	if err != nil {
		t.Fatalf("RandomHex: %v", err)
	}
	b, _ := RandomHex(16)
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("lengths = %d/%d, want 32", len(a), len(b))
	}
	if a == b {
		t.Fatal("two draws collided")
	}
}
