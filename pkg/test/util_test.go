package test

import (
	"testing"
)

func TestSetForTest(t *testing.T) {
	v := 1
	t.Run("subtest", func(t *testing.T) {
		SetIntForTest(t, &v, 2)
		if v != 2 {
			t.Fatalf("unexpected v: %v", v)
		}
	})
	if v != 1 {
		t.Fatalf("unexpected v: %v", v)
	}
}
