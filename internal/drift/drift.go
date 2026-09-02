// Package drift reads live EKS add-on versions and classifies each against the
// versions AWS reports as compatible with the cluster's Kubernetes version.
package drift

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/moveeeax/eks-addon-drift/internal/version"
)

// Class is the drift verdict for a single add-on.
type Class string

const (
	// ClassOK means the add-on is on the latest compatible version.
	ClassOK Class = "ok"
	// ClassBehind means the add-on is older but still compatible.
	ClassBehind Class = "behind"
	// ClassUnsupported means the running version is not in the compatible set
	// for the cluster's Kubernetes version.
	ClassUnsupported Class = "unsupported"
)

// severity orders classes so --fail-on can gate on "at least this bad".
func (c Class) severity() int {
	switch c {
	case ClassUnsupported:
		return 2
	case ClassBehind:
		return 1
	default:
		return 0
	}
}

// AtLeast reports whether c is at least as severe as other.
func (c Class) AtLeast(other Class) bool { return c.severity() >= other.severity() }

// Finding is the drift verdict for one add-on on one cluster.
type Finding struct {
	Cluster        string `json:"cluster"`
	ClusterVersion string `json:"clusterVersion"`
	Addon          string `json:"addon"`
	Current        string `json:"current"`
	Recommended    string `json:"recommended"`
	Class          Class  `json:"class"`
}

// EKSAPI is the read-only slice of the EKS API this tool needs. The concrete
// *eks.Client satisfies it directly; tests use a fake.
type EKSAPI interface {
	ListClusters(context.Context, *eks.ListClustersInput, ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	ListAddons(context.Context, *eks.ListAddonsInput, ...func(*eks.Options)) (*eks.ListAddonsOutput, error)
	DescribeAddon(context.Context, *eks.DescribeAddonInput, ...func(*eks.Options)) (*eks.DescribeAddonOutput, error)
	DescribeAddonVersions(context.Context, *eks.DescribeAddonVersionsInput, ...func(*eks.Options)) (*eks.DescribeAddonVersionsOutput, error)
}

// Analyzer runs the drift check against an EKS API.
type Analyzer struct {
	api EKSAPI
}

// New builds an Analyzer over the given EKS API.
func New(api EKSAPI) *Analyzer { return &Analyzer{api: api} }

// Analyze inspects the named clusters (or every cluster in the account/region
// when clusters is empty) and returns one Finding per installed managed add-on,
// sorted by cluster then add-on for stable output.
func (a *Analyzer) Analyze(ctx context.Context, clusters []string) ([]Finding, error) {
	names := clusters
	if len(names) == 0 {
		var err error
		names, err = a.listClusters(ctx)
		if err != nil {
			return nil, err
		}
	}

	var findings []Finding
	for _, name := range names {
		dc, err := a.api.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
		if err != nil {
			return nil, fmt.Errorf("describe cluster %s: %w", name, err)
		}
		if dc.Cluster == nil || dc.Cluster.Version == nil {
			return nil, fmt.Errorf("cluster %s: no Kubernetes version reported", name)
		}
		k8s := aws.ToString(dc.Cluster.Version)

		addons, err := a.listAddons(ctx, name)
		if err != nil {
			return nil, err
		}
		for _, addon := range addons {
			f, err := a.analyzeAddon(ctx, name, k8s, addon)
			if err != nil {
				return nil, err
			}
			findings = append(findings, f)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Cluster != findings[j].Cluster {
			return findings[i].Cluster < findings[j].Cluster
		}
		return findings[i].Addon < findings[j].Addon
	})
	return findings, nil
}

func (a *Analyzer) analyzeAddon(ctx context.Context, cluster, k8s, addon string) (Finding, error) {
	da, err := a.api.DescribeAddon(ctx, &eks.DescribeAddonInput{
		ClusterName: aws.String(cluster),
		AddonName:   aws.String(addon),
	})
	if err != nil {
		return Finding{}, fmt.Errorf("describe addon %s/%s: %w", cluster, addon, err)
	}
	current := ""
	if da.Addon != nil {
		current = aws.ToString(da.Addon.AddonVersion)
	}

	compatible, err := a.compatibleVersions(ctx, addon, k8s)
	if err != nil {
		return Finding{}, err
	}
	recommended := version.Max(compatible)

	return Finding{
		Cluster:        cluster,
		ClusterVersion: k8s,
		Addon:          addon,
		Current:        current,
		Recommended:    recommended,
		Class:          classify(current, recommended, compatible),
	}, nil
}

// classify decides ok/behind/unsupported from the current version and the set
// of versions AWS reports compatible with the cluster's Kubernetes version.
func classify(current, recommended string, compatible []string) Class {
	inSet := false
	for _, v := range compatible {
		if v == current {
			inSet = true
			break
		}
	}
	if !inSet {
		return ClassUnsupported
	}
	if version.Compare(current, recommended) >= 0 {
		return ClassOK
	}
	return ClassBehind
}

// compatibleVersions returns every add-on version AWS marks compatible with the
// given Kubernetes version, deduplicated.
func (a *Analyzer) compatibleVersions(ctx context.Context, addon, k8s string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	var next *string
	for {
		resp, err := a.api.DescribeAddonVersions(ctx, &eks.DescribeAddonVersionsInput{
			AddonName:         aws.String(addon),
			KubernetesVersion: aws.String(k8s),
			NextToken:         next,
		})
		if err != nil {
			return nil, fmt.Errorf("describe addon versions %s (k8s %s): %w", addon, k8s, err)
		}
		for _, info := range resp.Addons {
			for _, av := range info.AddonVersions {
				v := aws.ToString(av.AddonVersion)
				if v != "" && !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
		if resp.NextToken == nil {
			break
		}
		next = resp.NextToken
	}
	return out, nil
}

func (a *Analyzer) listClusters(ctx context.Context) ([]string, error) {
	var out []string
	var next *string
	for {
		resp, err := a.api.ListClusters(ctx, &eks.ListClustersInput{NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("list clusters: %w", err)
		}
		out = append(out, resp.Clusters...)
		if resp.NextToken == nil {
			break
		}
		next = resp.NextToken
	}
	return out, nil
}

func (a *Analyzer) listAddons(ctx context.Context, cluster string) ([]string, error) {
	var out []string
	var next *string
	for {
		resp, err := a.api.ListAddons(ctx, &eks.ListAddonsInput{
			ClusterName: aws.String(cluster),
			NextToken:   next,
		})
		if err != nil {
			return nil, fmt.Errorf("list addons for %s: %w", cluster, err)
		}
		out = append(out, resp.Addons...)
		if resp.NextToken == nil {
			break
		}
		next = resp.NextToken
	}
	return out, nil
}

// Summary counts findings per class.
type Summary struct {
	OK          int `json:"ok"`
	Behind      int `json:"behind"`
	Unsupported int `json:"unsupported"`
}

// Summarize tallies findings by class.
func Summarize(findings []Finding) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Class {
		case ClassUnsupported:
			s.Unsupported++
		case ClassBehind:
			s.Behind++
		default:
			s.OK++
		}
	}
	return s
}

// ShouldFail reports whether any finding is at least as severe as threshold.
// A threshold of ClassOK never fails.
func ShouldFail(findings []Finding, threshold Class) bool {
	if threshold == ClassOK {
		return false
	}
	for _, f := range findings {
		if f.Class.AtLeast(threshold) {
			return true
		}
	}
	return false
}
