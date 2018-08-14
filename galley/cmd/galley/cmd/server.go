// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"github.com/spf13/cobra"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/version"
)

func serverCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	sa := server.DefaultArgs()

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Starts Galley as a server",
		Run: func(cmd *cobra.Command, args []string) {
			err := runServer(sa, printf, fatalf)
			if err != nil {
				fatalf("Error during startup: %v", err)
			}
		},
	}

	cmd.PersistentFlags().StringVarP(&sa.KubeConfig, "kubeConfig", "", sa.KubeConfig,
		"Path to the Kube config file")
	cmd.PersistentFlags().DurationVarP(&sa.ResyncPeriod, "resyncPeriod", "", sa.ResyncPeriod,
		"The resync duration for Kubernetes watchers")
	cmd.PersistentFlags().StringVarP(&sa.APIAddress, "address", "", sa.APIAddress,
		"Address to use for Galley's gRPC API, e.g. tcp://127.0.0.1:9092 or unix:///path/to/file")
	cmd.PersistentFlags().UintVarP(&sa.MaxReceivedMessageSize, "maxReceivedMessageSize", "", sa.MaxReceivedMessageSize,
		"Maximum size of individual gRPC messages")
	cmd.PersistentFlags().UintVarP(&sa.MaxConcurrentStreams, "maxConcurrentStreams", "", sa.MaxConcurrentStreams,
		"Maximum number of outstanding RPCs per connection")
	cmd.PersistentFlags().BoolVarP(&sa.Insecure, "insecure", "", sa.Insecure,
		"Use insecure gRPC communication")
	cmd.PersistentFlags().StringVarP(&sa.CertificateFile, "certFile", "", sa.CertificateFile,
		"The location of the certificate file for mutual TLS")
	cmd.PersistentFlags().StringVarP(&sa.KeyFile, "keyFile", "", sa.KeyFile,
		"The location of the key file for mutual TLS")
	cmd.PersistentFlags().StringVarP(&sa.CACertificateFile, "caCertFile", "", sa.CACertificateFile,
		"The location of the certificate file for the root certificate authority")
	cmd.PersistentFlags().StringVarP(&sa.AccessListFile, "accessListFile", "", sa.AccessListFile,
		"The access list yaml file that contains the allowd mTLS peer ids.")
	cmd.PersistentFlags().StringVar(&sa.LivenessProbeOptions.Path, "livenessProbePath", sa.LivenessProbeOptions.Path,
		"Path to the file for the liveness probe.")
	cmd.PersistentFlags().DurationVar(&sa.LivenessProbeOptions.UpdateInterval, "livenessProbeInterval", sa.LivenessProbeOptions.UpdateInterval,
		"Interval of updating file for the liveness probe.")
	cmd.PersistentFlags().StringVar(&sa.ReadinessProbeOptions.Path, "readinessProbePath", sa.ReadinessProbeOptions.Path,
		"Path to the file for the readiness probe.")
	cmd.PersistentFlags().DurationVar(&sa.ReadinessProbeOptions.UpdateInterval, "readinessProbeInterval", sa.ReadinessProbeOptions.UpdateInterval,
		"Interval of updating file for the readiness probe.")

	sa.LoggingOptions.AttachCobraFlags(cmd)
	sa.IntrospectionOptions.AttachCobraFlags(cmd)

	return cmd
}

func runServer(sa *server.Args, printf, fatalf shared.FormatFn) error {
	printf("Galley started with\n%s", sa)

	s, err := server.New(sa)
	if err != nil {
		fatalf("Unable to initialize Galley: %v", err)
	}

	printf("Istio Galley: %s", version.Info)
	printf("Starting gRPC server on %v", sa.APIAddress)

	s.Run()
	err = s.Wait()
	if err != nil {
		fatalf("Galley unexpectedly terminated: %v", err)
	}

	_ = s.Close()
	return nil
}
