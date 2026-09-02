// Package cmd wires the eks-addon-drift CLI.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "eks-addon-drift",
		Short: "Flag EKS managed add-ons that drift from the latest compatible version",
		Long: "eks-addon-drift reads the live versions of your EKS managed add-ons\n" +
			"(coredns, kube-proxy, vpc-cni, ...) and compares them against the versions\n" +
			"AWS reports compatible with each cluster's Kubernetes version. It is read-only:\n" +
			"only EKS List/Describe calls, safe to run in CI.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newCheckCmd())
	return root
}
