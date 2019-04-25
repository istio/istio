package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	istioVersion "istio.io/istio/pkg/version"
)

func newVersionCommand() *cobra.Command {
	versionCmd := istioVersion.CobraCommandWithOptions(istioVersion.CobraOptions{GetRemoteVersion: getRemoteInfo})
	versionCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "short" {
			flag.Value.Set("true")
		}
		if flag.Name == "remote" {
			flag.Value.Set("true")
		}
	})
	return versionCmd
}
