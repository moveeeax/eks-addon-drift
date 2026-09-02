package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/spf13/cobra"

	"github.com/moveeeax/eks-addon-drift/internal/drift"
	"github.com/moveeeax/eks-addon-drift/internal/output"
)

func newCheckCmd() *cobra.Command {
	var (
		clusters []string
		region   string
		format   string
		failOn   string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check EKS add-on drift across one or many clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := parseFailOn(failOn)
			if err != nil {
				return err
			}
			if format != "table" && format != "json" && format != "sarif" {
				return fmt.Errorf("invalid --output %q: want table, json, or sarif", format)
			}

			ctx := context.Background()
			opts := []func(*awsconfig.LoadOptions) error{}
			if region != "" {
				opts = append(opts, awsconfig.WithRegion(region))
			}
			cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
			if err != nil {
				return fmt.Errorf("load AWS config: %w", err)
			}

			findings, err := drift.New(eks.NewFromConfig(cfg)).Analyze(ctx, clusters)
			if err != nil {
				return err
			}

			if err := render(cmd.OutOrStdout(), format, findings); err != nil {
				return err
			}
			if drift.ShouldFail(findings, threshold) {
				os.Exit(2)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&clusters, "cluster", nil, "cluster name to check (repeatable); default: every cluster in the region")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (default: from environment/profile)")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output format: table, json, sarif")
	cmd.Flags().StringVar(&failOn, "fail-on", "none", "exit non-zero when a finding is at least this severe: none, behind, unsupported")
	return cmd
}

func render(w interface{ Write([]byte) (int, error) }, format string, findings []drift.Finding) error {
	switch format {
	case "json":
		return output.JSON(w, findings)
	case "sarif":
		return output.SARIF(w, findings)
	default:
		return output.Table(w, findings)
	}
}

func parseFailOn(s string) (drift.Class, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return drift.ClassOK, nil
	case "behind":
		return drift.ClassBehind, nil
	case "unsupported":
		return drift.ClassUnsupported, nil
	default:
		return "", fmt.Errorf("invalid --fail-on %q: want none, behind, or unsupported", s)
	}
}
