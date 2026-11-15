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

func targetSample() []drift.Finding {
	yes, no := true, false
	fs := sample()
	fs[0].TargetVersion, fs[0].TargetCompatible, fs[0].TargetRecommended = "1.30", &yes, "v1.11.3-eksbuild.1"
	fs[1].TargetVersion, fs[1].TargetCompatible, fs[1].TargetRecommended = "1.30", &no, "v1.30.0-eksbuild.3"
	fs[1].Class = drift.ClassBlocksUpgrade
	fs[2].TargetVersion, fs[2].TargetCompatible, fs[2].TargetRecommended = "1.30", &no, "v1.18.3-eksbuild.2"
	return fs
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

func TestJSONOmitsTargetWhenUnset(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sample()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	for _, key := range []string{"targetVersion", "targetCompatible", "targetRecommended"} {
		if strings.Contains(buf.String(), key) {
			t.Errorf("JSON without a target should not contain %q", key)
		}
	}
}

func TestJSONTargetFields(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, targetSample()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var r Report
	if err := json.Unmarshal(buf.Bytes(), &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	f := r.Findings[1]
	if f.TargetVersion != "1.30" || f.TargetCompatible == nil || *f.TargetCompatible || f.Class != drift.ClassBlocksUpgrade {
		t.Fatalf("kube-proxy target fields = %+v", f)
	}
	if r.Summary.BlocksUpgrade != 1 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	if !strings.Contains(buf.String(), `"targetCompatible": true`) {
		t.Errorf("targetCompatible true must be encoded, got: %s", buf.String())
	}
}

func TestTableTargetColumns(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, targetSample()); err != nil {
		t.Fatalf("Table: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"TARGET-COMPATIBLE", "TARGET-RECOMMENDED", "blocks-upgrade", "v1.30.0-eksbuild.3",
		"1 ok, 0 behind, 1 blocks-upgrade, 1 unsupported"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
}

func TestTableNoTargetColumnsByDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, sample()); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if strings.Contains(buf.String(), "TARGET") {
		t.Errorf("table without a target should not show TARGET columns\n%s", buf.String())
	}
}

func TestSARIFBlocksUpgrade(t *testing.T) {
	var buf bytes.Buffer
	if err := SARIF(&buf, targetSample()); err != nil {
		t.Fatalf("SARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	var found bool
	for _, r := range log.Runs[0].Results {
		if r.RuleID == "addon-blocks-upgrade" {
			found = true
			if r.Level != "error" || !strings.Contains(r.Message.Text, "target Kubernetes 1.30") {
				t.Errorf("blocks-upgrade result = %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("no addon-blocks-upgrade result")
	}
}
