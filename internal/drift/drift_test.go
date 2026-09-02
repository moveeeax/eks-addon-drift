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
		{Class: ClassOK}, {Class: ClassBehind}, {Class: ClassBehind}, {Class: ClassUnsupported},
	})
	if s.OK != 1 || s.Behind != 2 || s.Unsupported != 1 {
		t.Fatalf("summary = %+v", s)
	}
}
