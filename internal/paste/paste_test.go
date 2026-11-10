package paste

import (
	"context"
	"testing"
)

func TestTextEmptyIsNoop(t *testing.T) {
	if err := Text(context.Background(), ""); err != nil {
		t.Fatalf("empty text should be a no-op: %v", err)
	}
}
