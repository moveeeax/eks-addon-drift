package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moveeeax/eks-addon-drift/internal/drift"
)

func sample() []drift.Finding {
	return []drift.Finding{
		{Cluster: "prod", ClusterVersion: "1.29", Addon: "coredns", Current: "v1.11.1-eksbuild.9", Recommended: "v1.11.1-eksbuild.9", Class: drift.ClassOK},
		{Cluster: "prod", ClusterVersion: "1.29", Addon: "kube-proxy", Current: "v1.29.0-eksbuild.1", Recommended: "v1.29.3-eksbuild.2", Class: drift.ClassBehind},
		{Cluster: "prod", ClusterVersion: "1.29", Addon: "vpc-cni", Current: "v1.99.0-eksbuild.1", Recommended: "v1.18.3-eksbuild.2", Class: drift.ClassUnsupported},
	}
}

func TestJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sample()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var r Report
	if err := json.Unmarshal(buf.Bytes(), &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(r.Findings) != 3 {
		t.Fatalf("want 3 findings, got %d", len(r.Findings))
	}
	if r.Summary.OK != 1 || r.Summary.Behind != 1 || r.Summary.Unsupported != 1 {
		t.Fatalf("summary = %+v", r.Summary)
	}
}

func TestJSONEmptyIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, nil); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"findings": []`) {
		t.Fatalf("empty findings should encode as [], got: %s", buf.String())
	}
}

func TestSARIFValidShape(t *testing.T) {
	var buf bytes.Buffer
	if err := SARIF(&buf, sample()); err != nil {
		t.Fatalf("SARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(log.Runs))
	}
	// Only behind + unsupported become results; ok is dropped.
	if got := len(log.Runs[0].Results); got != 2 {
		t.Fatalf("want 2 results, got %d", got)
	}
	levels := map[string]int{}
	for _, r := range log.Runs[0].Results {
		levels[r.Level]++
	}
	if levels["warning"] != 1 || levels["error"] != 1 {
		t.Fatalf("levels = %+v, want 1 warning + 1 error", levels)
	}
}

func TestTableContainsRows(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, sample()); err != nil {
		t.Fatalf("Table: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"CLUSTER", "kube-proxy", "unsupported", "1 ok, 1 behind, 1 unsupported"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
}
