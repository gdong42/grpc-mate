package log

import (
	"testing"
)

func TestNewLogger(t *testing.T) {
	if _, err := NewLogger("INFO"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLogger("INVALID"); err == nil {
		t.Fatal(err)
	}
}
