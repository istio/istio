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

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/log"
	ca "istio.io/istio/security/pkg/nodeagent/caClient"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

var (
	cacheOptions  cache.Options
	serverOptions sds.Options

	// RootCmd defines the command for node agent.
	RootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
		RunE: func(c *cobra.Command, args []string) error {
			stop := make(chan struct{})

			caClient, err := ca.NewCAClient(serverOptions.CAEndpoint, serverOptions.CARootFile)
			if err != nil {
				log.Errorf("failed to create caClient: %v", err)
				return fmt.Errorf("failed to create caClient")
			}
			sc := cache.NewSecretCache(caClient, sds.NotifyProxy, cacheOptions)
			defer sc.Close()

			server, err := sds.NewServer(serverOptions, sc)
			defer server.Stop()
			if err != nil {
				log.Errorf("failed to create sds service: %v", err)
				return fmt.Errorf("failed to create sds service")
			}

			cmd.WaitSignal(stop)

			return nil
		},
	}
)

func init() {
	RootCmd.PersistentFlags().StringVar(&serverOptions.UDSPath, "sdsUdsPath",
		"/var/run/sds/uds_path", "Unix domain socket through which SDS server communicates with proxies")

	RootCmd.PersistentFlags().StringVar(&serverOptions.CAEndpoint, "caEndpoint", "prod-istioca.sandbox.googleapis.com:443", "CA endpoint")
	RootCmd.PersistentFlags().StringVar(&serverOptions.CARootFile, "caRootFile", "/etc/istio/roots.pem",
		"path of CA file for setup channel credential to CA endpoint.")

	//local test through TLS using '/etc/istio/nodeagent-sds-cert.pem' and '/etc/istio/nodeagent-sds-key.pem'
	RootCmd.PersistentFlags().StringVar(&serverOptions.CertFile, "sdsCertFile", "", "SDS gRPC TLS server-side certificate")
	RootCmd.PersistentFlags().StringVar(&serverOptions.KeyFile, "sdsKeyFile", "", "SDS gRPC TLS server-side key")

	RootCmd.PersistentFlags().DurationVar(&cacheOptions.SecretTTL, "secretTtl",
		time.Hour, "Secret's TTL")
	RootCmd.PersistentFlags().DurationVar(&cacheOptions.RotationInterval, "secretRotationInterval",
		10*time.Minute, "Secret rotation job running interval")
	RootCmd.PersistentFlags().DurationVar(&cacheOptions.EvictionDuration, "secretEvictionDuration",
		24*time.Hour, "Secret eviction time duration")
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
