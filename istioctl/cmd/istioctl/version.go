package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	istioVersion "istio.io/istio/pkg/version"
)

func newVersionCommand() *cobra.Command {
	versionCmd := istioVersion.CobraCommandWithOptions(istioVersion.CobraOptions{GetRemoteVersion: getRemoteInfo})
	versionCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "short" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprint(os.Stdout, fmt.Sprintf("set flag %q as true failed due to error %v", flag.Name, err))
			}
		}
		if flag.Name == "remote" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprint(os.Stdout, fmt.Sprintf("set flag %q as true failed due to error %v", flag.Name, err))
			}
		}
	})
	return versionCmd
}
