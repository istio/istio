package main

import (
	"flag"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	k8sserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/util/logs"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/server"
)

const (
	defaultWakeupInterval = 5 * time.Minute
)

// config flags defined globally so that they appear on the test binary as well
var (
	stopCh  = k8sserver.SetupSignalHandler()
	options = server.NewPackageServerOptions(os.Stdout, os.Stderr)
	cmd     = &cobra.Command{
		Short: "Launch a package-server",
		Long:  "Launch a package-server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := options.Run(stopCh); err != nil {
				return err
			}
			return nil
		},
	}
)

func init() {
	flags := cmd.Flags()

	flags.DurationVar(&options.WakeupInterval, "interval", options.WakeupInterval, "Interval at which to re-sync CatalogSources")
	flags.StringVar(&options.GlobalNamespace, "global-namespace", options.GlobalNamespace, "Name of the namespace where the global CatalogSources are located")
	flags.StringSliceVar(&options.WatchedNamespaces, "watched-namespaces", options.WatchedNamespaces, "List of namespaces the package-server will watch watch for CatalogSources")
	flags.StringVar(&options.Kubeconfig, "kubeconfig", options.Kubeconfig, "The path to the kubeconfig used to connect to the Kubernetes API server and the Kubelets (defaults to in-cluster config)")
	flags.BoolVar(&options.Debug, "debug", options.Debug, "use debug log level")

	options.SecureServing.AddFlags(flags)
	options.Authentication.AddFlags(flags)
	options.Authorization.AddFlags(flags)
	options.Features.AddFlags(flags)

	flags.AddGoFlagSet(flag.CommandLine)
	flags.Parse(flag.Args())
}

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
