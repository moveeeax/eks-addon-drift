package main

import (
	"fmt"
	"os"

	"github.com/moveeeax/eks-addon-drift/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "eks-addon-drift:", err)
		os.Exit(1)
	}
}
