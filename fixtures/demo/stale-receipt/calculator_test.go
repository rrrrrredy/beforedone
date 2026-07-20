package calculator

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(20, 22); got != 42 {
		t.Fatalf("Add(20, 22) = %d, want 42", got)
	}
}
