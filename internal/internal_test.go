package internal

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReadAllLimited(t *testing.T) {
	t.Parallel()

	got, err := ReadAllLimited(strings.NewReader("hello"), 16)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}

	_, err = ReadAllLimited(strings.NewReader(strings.Repeat("a", 8)), 4)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}

	got, err = ReadAllLimited(bytes.NewReader([]byte("abc")), 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc" {
		t.Fatalf("unlimited read got %q", got)
	}
}
