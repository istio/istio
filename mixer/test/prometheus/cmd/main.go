package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/test/prometheus"
)

type Args struct {
	// Port to start the grpc adapter on
	AdapterPort uint16

	// Port to use for the prometheus endpoint
	PrometheusPort uint16
}

func defaultArgs() *Args {
	return &Args{
		AdapterPort:    uint16(8080),
		PrometheusPort: uint16(42422),
	}
}

// GetCmd returns the cobra command-tree.
func GetCmd(args []string) *cobra.Command {
	sa := defaultArgs()
	cmd := &cobra.Command{
		Use:   "prometheus",
		Short: "Prometheus out of process adapter.",
		Run: func(cmd *cobra.Command, args []string) {
			runServer(sa)
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}

	f := cmd.PersistentFlags()
	f.Uint16VarP(&sa.AdapterPort, "port", "p", sa.AdapterPort,
		"TCP port to use for gRPC Adapter API")
	f.Uint16VarP(&sa.PrometheusPort, "prometheusport", "a", sa.PrometheusPort,
		"TCP port to expose prometheus endpoint on")

	return cmd
}

func main() {
	cmd := GetCmd(os.Args[1:])
	if err := cmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

func runServer(args *Args) {
	s, err := prometheus.NewNoSessionServer(args.AdapterPort, args.PrometheusPort)
	if err != nil {
		fmt.Printf("unable to start sever: %v", err)
		os.Exit(-1)
	}

	s.Run()
	s.Wait()
}
