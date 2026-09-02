package version

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.11.1-eksbuild.9", "v1.11.1-eksbuild.9", 0},
		{"v1.11.1-eksbuild.4", "v1.11.1-eksbuild.9", -1}, // build counter
		{"v1.11.1-eksbuild.9", "v1.11.1-eksbuild.4", 1},
		{"v1.10.1-eksbuild.11", "v1.11.1-eksbuild.1", -1}, // minor beats build
		{"v1.29.3-eksbuild.2", "v1.29.0-eksbuild.9", 1},   // patch beats build
		{"1.18.1-eksbuild.1", "v1.18.1-eksbuild.1", 0},    // leading v optional
		{"v2.0.0", "v1.99.99-eksbuild.99", 1},             // major dominates
		{"v1.11.1", "v1.11.1-eksbuild.1", -1},             // missing build == 0
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMax(t *testing.T) {
	got := Max([]string{"v1.11.1-eksbuild.4", "v1.11.1-eksbuild.9", "v1.10.1-eksbuild.11"})
	if got != "v1.11.1-eksbuild.9" {
		t.Errorf("Max = %q, want v1.11.1-eksbuild.9", got)
	}
	if Max(nil) != "" {
		t.Errorf("Max(nil) = %q, want empty", Max(nil))
	}
}

func TestCompareUnparseable(t *testing.T) {
	// A parseable version always outranks an unparseable one.
	if Compare("v1.0.0", "garbage") <= 0 {
		t.Error("parseable version should outrank unparseable")
	}
	if Compare("garbage", "v1.0.0") >= 0 {
		t.Error("unparseable version should sort below parseable")
	}
}
