package drift

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// fakeEKS is an in-memory EKSAPI for tests. It supports pagination for
// ListClusters so the paging loop is exercised, and is keyed by cluster/addon.
type fakeEKS struct {
	clusters       []string
	pageClusters   bool // return the cluster list one item per page
	clusterVersion map[string]string
	addons         map[string][]string            // cluster -> addon names
	current        map[string]map[string]string   // cluster -> addon -> current version
	compatible     map[string]map[string][]string // k8s -> addon -> compatible versions
}

func (f *fakeEKS) ListClusters(_ context.Context, in *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	if !f.pageClusters {
		return &eks.ListClustersOutput{Clusters: f.clusters}, nil
	}
	start := 0
	if in.NextToken != nil {
		start = int((*in.NextToken)[0] - '0')
	}
	if start >= len(f.clusters) {
		return &eks.ListClustersOutput{}, nil
	}
	out := &eks.ListClustersOutput{Clusters: []string{f.clusters[start]}}
	if start+1 < len(f.clusters) {
		tok := string(rune('0' + start + 1))
		out.NextToken = aws.String(tok)
	}
	return out, nil
}

func (f *fakeEKS) DescribeCluster(_ context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	v := f.clusterVersion[aws.ToString(in.Name)]
	return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String(v)}}, nil
}

func (f *fakeEKS) ListAddons(_ context.Context, in *eks.ListAddonsInput, _ ...func(*eks.Options)) (*eks.ListAddonsOutput, error) {
	return &eks.ListAddonsOutput{Addons: f.addons[aws.ToString(in.ClusterName)]}, nil
}

func (f *fakeEKS) DescribeAddon(_ context.Context, in *eks.DescribeAddonInput, _ ...func(*eks.Options)) (*eks.DescribeAddonOutput, error) {
	cluster := aws.ToString(in.ClusterName)
	addon := aws.ToString(in.AddonName)
	return &eks.DescribeAddonOutput{Addon: &ekstypes.Addon{
		AddonName:    aws.String(addon),
		AddonVersion: aws.String(f.current[cluster][addon]),
	}}, nil
}

func (f *fakeEKS) DescribeAddonVersions(_ context.Context, in *eks.DescribeAddonVersionsInput, _ ...func(*eks.Options)) (*eks.DescribeAddonVersionsOutput, error) {
	k8s := aws.ToString(in.KubernetesVersion)
	addon := aws.ToString(in.AddonName)
	if addon == "" {
		// Unscoped query: one AddonInfo per add-on offered for this version.
		var infos []ekstypes.AddonInfo
		for name := range f.compatible[k8s] {
			infos = append(infos, ekstypes.AddonInfo{AddonName: aws.String(name)})
		}
		return &eks.DescribeAddonVersionsOutput{Addons: infos}, nil
	}
	var versions []ekstypes.AddonVersionInfo
	for _, v := range f.compatible[k8s][addon] {
		versions = append(versions, ekstypes.AddonVersionInfo{AddonVersion: aws.String(v)})
	}
	return &eks.DescribeAddonVersionsOutput{
		Addons: []ekstypes.AddonInfo{{AddonName: aws.String(addon), AddonVersions: versions}},
	}, nil
}

func newFake() *fakeEKS {
	return &fakeEKS{
		clusters:       []string{"prod", "staging"},
		clusterVersion: map[string]string{"prod": "1.29", "staging": "1.29"},
		addons: map[string][]string{
			"prod":    {"coredns", "kube-proxy", "vpc-cni"},
			"staging": {"coredns"},
		},
		current: map[string]map[string]string{
			"prod": {
				"coredns":    "v1.11.1-eksbuild.9", // latest -> ok
				"kube-proxy": "v1.29.0-eksbuild.1", // older -> behind
				"vpc-cni":    "v1.99.0-eksbuild.1", // not in set -> unsupported
			},
			"staging": {"coredns": "v1.11.1-eksbuild.4"}, // older -> behind
		},
		compatible: map[string]map[string][]string{
			"1.29": {
				"coredns":    {"v1.11.1-eksbuild.4", "v1.11.1-eksbuild.9", "v1.10.1-eksbuild.11"},
				"kube-proxy": {"v1.29.0-eksbuild.1", "v1.29.3-eksbuild.2"},
				"vpc-cni":    {"v1.18.1-eksbuild.1", "v1.18.3-eksbuild.2"},
			},
		},
	}
}

func classOf(t *testing.T, fs []Finding, cluster, addon string) Finding {
	t.Helper()
	for _, f := range fs {
		if f.Cluster == cluster && f.Addon == addon {
			return f
		}
	}
	t.Fatalf("no finding for %s/%s", cluster, addon)
	return Finding{}
}

func TestAnalyzeClassification(t *testing.T) {
	findings, err := New(newFake()).Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(findings) != 4 {
		t.Fatalf("want 4 findings, got %d: %+v", len(findings), findings)
	}

	if got := classOf(t, findings, "prod", "coredns"); got.Class != ClassOK {
		t.Errorf("prod/coredns: want ok, got %s", got.Class)
	}
	if got := classOf(t, findings, "prod", "kube-proxy"); got.Class != ClassBehind {
		t.Errorf("prod/kube-proxy: want behind, got %s", got.Class)
	} else if got.Recommended != "v1.29.3-eksbuild.2" {
		t.Errorf("prod/kube-proxy: recommended = %q, want v1.29.3-eksbuild.2", got.Recommended)
	}
	if got := classOf(t, findings, "prod", "vpc-cni"); got.Class != ClassUnsupported {
		t.Errorf("prod/vpc-cni: want unsupported, got %s", got.Class)
	}
	if got := classOf(t, findings, "staging", "coredns"); got.Class != ClassBehind {
		t.Errorf("staging/coredns: want behind, got %s", got.Class)
	}
}

func TestAnalyzeSortedStable(t *testing.T) {
	findings, err := New(newFake()).Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// prod sorts before staging; within prod, addons are alphabetical.
	want := []struct{ c, a string }{
		{"prod", "coredns"}, {"prod", "kube-proxy"}, {"prod", "vpc-cni"}, {"staging", "coredns"},
	}
	for i, w := range want {
		if findings[i].Cluster != w.c || findings[i].Addon != w.a {
			t.Fatalf("finding[%d] = %s/%s, want %s/%s", i, findings[i].Cluster, findings[i].Addon, w.c, w.a)
		}
	}
}

func TestAnalyzePaginatesClusters(t *testing.T) {
	f := newFake()
	f.pageClusters = true
	findings, err := New(f).Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(findings) != 4 {
		t.Fatalf("pagination lost clusters: want 4 findings, got %d", len(findings))
	}
}

func TestAnalyzeExplicitClusterSubset(t *testing.T) {
	findings, err := New(newFake()).Analyze(context.Background(), []string{"staging"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(findings) != 1 || findings[0].Cluster != "staging" {
		t.Fatalf("explicit subset ignored: %+v", findings)
	}
}

func TestShouldFail(t *testing.T) {
	findings := []Finding{
		{Class: ClassOK}, {Class: ClassBehind},
	}
	if ShouldFail(findings, ClassOK) {
		t.Error("threshold ok must never fail")
	}
	if !ShouldFail(findings, ClassBehind) {
		t.Error("behind present, threshold behind: want fail")
	}
	if ShouldFail(findings, ClassUnsupported) {
		t.Error("no unsupported, threshold unsupported: want no fail")
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]Finding{
		{Class: ClassOK}, {Class: ClassBehind}, {Class: ClassBehind}, {Class: ClassBlocksUpgrade}, {Class: ClassUnsupported},
	})
	if s.OK != 1 || s.Behind != 2 || s.BlocksUpgrade != 1 || s.Unsupported != 1 {
		t.Fatalf("summary = %+v", s)
	}
}

// withNext adds a 1.30 compatible set to the fake: coredns and vpc-cni carry
// over, kube-proxy needs a 1.30 build.
func withNext(f *fakeEKS) *fakeEKS {
	f.compatible["1.30"] = map[string][]string{
		"coredns":    {"v1.11.1-eksbuild.9", "v1.11.3-eksbuild.1"},
		"kube-proxy": {"v1.30.0-eksbuild.3"},
		"vpc-cni":    {"v1.18.3-eksbuild.2"},
	}
	return f
}

func TestAnalyzeTargetNext(t *testing.T) {
	findings, err := New(withNext(newFake())).WithTarget(TargetNext).Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	cases := []struct {
		cluster, addon string
		class          Class
		compatible     bool
		targetRec      string
	}{
		{"prod", "coredns", ClassOK, true, "v1.11.3-eksbuild.1"},
		{"prod", "kube-proxy", ClassBlocksUpgrade, false, "v1.30.0-eksbuild.3"},
		// Already broken today: unsupported wins over blocks-upgrade.
		{"prod", "vpc-cni", ClassUnsupported, false, "v1.18.3-eksbuild.2"},
		// Behind today and not carried over to 1.30.
		{"staging", "coredns", ClassBlocksUpgrade, false, "v1.11.3-eksbuild.1"},
	}
	for _, c := range cases {
		got := classOf(t, findings, c.cluster, c.addon)
		if got.TargetVersion != "1.30" {
			t.Errorf("%s/%s: targetVersion = %q, want 1.30", c.cluster, c.addon, got.TargetVersion)
		}
		if got.Class != c.class {
			t.Errorf("%s/%s: class = %s, want %s", c.cluster, c.addon, got.Class, c.class)
		}
		if got.TargetCompatible == nil || *got.TargetCompatible != c.compatible {
			t.Errorf("%s/%s: targetCompatible = %v, want %v", c.cluster, c.addon, got.TargetCompatible, c.compatible)
		}
		if got.TargetRecommended != c.targetRec {
			t.Errorf("%s/%s: targetRecommended = %q, want %q", c.cluster, c.addon, got.TargetRecommended, c.targetRec)
		}
	}
}

func TestAnalyzeTargetExplicit(t *testing.T) {
	f := withNext(newFake())
	f.clusterVersion["staging"] = "1.30"
	findings, err := New(f).WithTarget("1.30").Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := classOf(t, findings, "prod", "kube-proxy"); got.Class != ClassBlocksUpgrade {
		t.Errorf("prod/kube-proxy: class = %s, want blocks-upgrade", got.Class)
	}
	// staging is already on 1.30: no upgrade, so no target check.
	got := classOf(t, findings, "staging", "coredns")
	if got.HasTarget() || got.TargetCompatible != nil || got.TargetRecommended != "" {
		t.Errorf("staging/coredns: target fields set for a cluster already on the target: %+v", got)
	}
}

func TestAnalyzeNoTargetLeavesFieldsEmpty(t *testing.T) {
	findings, err := New(withNext(newFake())).Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	for _, f := range findings {
		if f.HasTarget() || f.TargetCompatible != nil || f.Class == ClassBlocksUpgrade {
			t.Errorf("%s/%s: target data without --target-version: %+v", f.Cluster, f.Addon, f)
		}
	}
}

func TestAnalyzeTargetNotOffered(t *testing.T) {
	// No 1.30 data in the fake: EKS offers nothing for that version.
	_, err := New(newFake()).WithTarget(TargetNext).Analyze(context.Background(), nil)
	if err == nil {
		t.Fatal("want an error for a target version EKS does not offer")
	}
}

func TestValidateTarget(t *testing.T) {
	for _, ok := range []string{"", "next", "1.31", " 1.31 "} {
		if err := ValidateTarget(ok); err != nil {
			t.Errorf("ValidateTarget(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"1", "1.31.0", "latest", "1.x", "-1.2"} {
		if err := ValidateTarget(bad); err == nil {
			t.Errorf("ValidateTarget(%q): want error", bad)
		}
	}
}

func TestShouldFailBlocksUpgrade(t *testing.T) {
	behind := []Finding{{Class: ClassBehind}}
	blocks := []Finding{{Class: ClassOK}, {Class: ClassBlocksUpgrade}}
	unsupported := []Finding{{Class: ClassUnsupported}}

	if ShouldFail(behind, ClassBlocksUpgrade) {
		t.Error("behind only, threshold blocks-upgrade: want no fail")
	}
	if !ShouldFail(blocks, ClassBlocksUpgrade) {
		t.Error("blocks-upgrade present, threshold blocks-upgrade: want fail")
	}
	if !ShouldFail(blocks, ClassBehind) {
		t.Error("blocks-upgrade present, threshold behind: want fail")
	}
	if ShouldFail(blocks, ClassUnsupported) {
		t.Error("blocks-upgrade only, threshold unsupported: want no fail")
	}
	if !ShouldFail(unsupported, ClassBlocksUpgrade) {
		t.Error("unsupported present, threshold blocks-upgrade: want fail")
	}
}
