package parsers

import (
	"regexp"
	"testing"
)

func TestMatchNameFilter(t *testing.T) {
	wl := mustCompile(t, "^foo_")
	bl := mustCompile(t, "bad")

	if !MatchNameFilter("foo_bar", []*regexp.Regexp{wl}, nil) {
		t.Fatal("expected foo_bar to pass whitelist")
	}
	if MatchNameFilter("bar_foo", []*regexp.Regexp{wl}, nil) {
		t.Fatal("expected bar_foo to fail whitelist")
	}
	if MatchNameFilter("foo_bad", []*regexp.Regexp{wl}, []*regexp.Regexp{bl}) {
		t.Fatal("expected foo_bad to fail blacklist")
	}
	if !MatchNameFilter("foo_ok", nil, []*regexp.Regexp{bl}) {
		t.Fatal("expected foo_ok to pass without blacklist match")
	}
}

func mustCompile(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return re
}
