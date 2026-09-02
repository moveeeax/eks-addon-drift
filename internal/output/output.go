// Package output renders drift findings as a table, JSON, or SARIF.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/moveeeax/eks-addon-drift/internal/drift"
)

// Table writes a human-readable, aligned table plus a one-line summary.
func Table(w io.Writer, findings []drift.Finding) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CLUSTER\tK8S\tADDON\tCURRENT\tRECOMMENDED\tSTATUS")
	for _, f := range findings {
		rec := f.Recommended
		if rec == "" {
			rec = "-"
		}
		cur := f.Current
		if cur == "" {
			cur = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Cluster, f.ClusterVersion, f.Addon, cur, rec, f.Class)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	s := drift.Summarize(findings)
	_, err := fmt.Fprintf(w, "\n%d ok, %d behind, %d unsupported\n", s.OK, s.Behind, s.Unsupported)
	return err
}

// Report is the top-level JSON document.
type Report struct {
	Summary  drift.Summary   `json:"summary"`
	Findings []drift.Finding `json:"findings"`
}

// JSON writes findings and a summary as indented JSON.
func JSON(w io.Writer, findings []drift.Finding) error {
	if findings == nil {
		findings = []drift.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Report{Summary: drift.Summarize(findings), Findings: findings})
}

// SARIF writes a SARIF 2.1.0 log. Only behind/unsupported add-ons become
// results (ok add-ons carry no finding), so the report drops cleanly into a
// code-scanning dashboard.
func SARIF(w io.Writer, findings []drift.Finding) error {
	rules := []sarifRule{
		{
			ID:               "addon-behind",
			Name:             "AddonBehind",
			ShortDescription: sarifText{Text: "Add-on is behind the latest compatible version"},
			DefaultConfig:    sarifConfig{Level: "warning"},
		},
		{
			ID:               "addon-unsupported",
			Name:             "AddonUnsupported",
			ShortDescription: sarifText{Text: "Add-on version is not compatible with the cluster Kubernetes version"},
			DefaultConfig:    sarifConfig{Level: "error"},
		},
	}

	results := []sarifResult{}
	for _, f := range findings {
		var ruleID, level, msg string
		switch f.Class {
		case drift.ClassBehind:
			ruleID, level = "addon-behind", "warning"
			msg = fmt.Sprintf("Add-on %s on cluster %s is at %s; latest compatible with Kubernetes %s is %s.",
				f.Addon, f.Cluster, f.Current, f.ClusterVersion, f.Recommended)
		case drift.ClassUnsupported:
			ruleID, level = "addon-unsupported", "error"
			rec := f.Recommended
			if rec == "" {
				rec = "none"
			}
			msg = fmt.Sprintf("Add-on %s on cluster %s runs %s, which is not compatible with Kubernetes %s (recommended: %s).",
				f.Addon, f.Cluster, f.Current, f.ClusterVersion, rec)
		default:
			continue
		}
		results = append(results, sarifResult{
			RuleID:  ruleID,
			Level:   level,
			Message: sarifText{Text: msg},
			Locations: []sarifLocation{{
				LogicalLocations: []sarifLogical{{
					Name:               f.Addon,
					FullyQualifiedName: f.Cluster + "/" + f.Addon,
					Kind:               "resource",
				}},
			}},
		})
	}

	log := sarifLog{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "eks-addon-drift",
				InformationURI: "https://github.com/moveeeax/eks-addon-drift",
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	ShortDescription sarifText   `json:"shortDescription"`
	DefaultConfig    sarifConfig `json:"defaultConfiguration"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	LogicalLocations []sarifLogical `json:"logicalLocations"`
}

type sarifLogical struct {
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName"`
	Kind               string `json:"kind"`
}

type sarifText struct {
	Text string `json:"text"`
}
