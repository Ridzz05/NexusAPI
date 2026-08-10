package migrations

import (
	"context"
	"testing"
)

func TestApplyRejectsNilPool(t *testing.T) {
	if err := Apply(context.Background(), nil); err == nil {
		t.Fatal("expected nil migration pool to be rejected")
	}
}
