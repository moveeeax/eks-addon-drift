package cmd

import (
	"testing"

	"github.com/moveeeax/eks-addon-drift/internal/drift"
)

func TestParseFailOn(t *testing.T) {
	cases := map[string]drift.Class{
		"":               drift.ClassOK,
		"none":           drift.ClassOK,
		"behind":         drift.ClassBehind,
		"blocks-upgrade": drift.ClassBlocksUpgrade,
		"Unsupported":    drift.ClassUnsupported,
	}
	for in, want := range cases {
		got, err := parseFailOn(in)
		if err != nil || got != want {
			t.Errorf("parseFailOn(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseFailOn("everything"); err == nil {
		t.Error("parseFailOn(everything): want error")
	}
}
