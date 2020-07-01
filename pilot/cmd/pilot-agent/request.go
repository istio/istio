// Copyright Istio Authors
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
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"istio.io/pkg/env"

	"istio.io/istio/pilot/pkg/request"
)

func init() {

	requestCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfig", defaultMeshConfigFile,
		"File name for Istio mesh configuration. If not specified, a default mesh will be used. This may be overridden by "+
			"PROXY_CONFIG environment variable or proxy.istio.io/config annotation.")
	proxyCmd.PersistentFlags().Int32Var(&envoyAdminPort, "envoyAdminPort", 0,
		"Port where Envoy listens (on local host) for admin commands. If zero, get from mesh configuration or environment variable.")
}

// NB: extra standard output in addition to what's returned from envoy
// must not be added in this command. Otherwise, it'd break istioctl proxy-config,
// which interprets the output literally as json document.
var (
	envoyAdminPort int32

	proxyAdminPortConfigEnv = env.RegisterIntVar(
		"ISTIO_PROXY_ADMIN_PORT",
		0,
		"The proxy configuration. This will be set by the injection - gateways will use file mounts.",
	).Get()

	requestCmd = &cobra.Command{
		Use:   "request <method> <path> [<body>]",
		Short: "Makes an HTTP request to the Envoy admin API",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			host, adminPort := getEnvoyHostAndAdminPort()
			command := &request.Command{
				Address: fmt.Sprintf("%s:%d", host, adminPort),
				Client: &http.Client{
					Timeout: 60 * time.Second,
				},
			}
			body := ""
			if len(args) >= 3 {
				body = args[2]
			}
			return command.Do(args[0], args[1], body)
		},
	}
)

func init() {
	rootCmd.AddCommand(requestCmd)
}

// get admin port from Istio mesh configuration , This may be overridden by "ISTIO_PROXY_ADMIN_PORT" variable
func getEnvoyHostAndAdminPort() (string, int32) {
	var adminHost = "localhost"
	var adminPort int32 = 15000

	port, err := getPortFromProxyConfig()
	if err == nil {
		adminPort = port
	}

	if proxyAdminPortConfigEnv != 0 {
		adminPort = int32(proxyAdminPortConfigEnv)
	}

	return adminHost, adminPort
}

// then deafault meshConfigFile path is ./etc/istio/config/mesh
// File name for Istio mesh configuration in flags. If not specified, a default mesh will be used. This may be overridden by
// "PROXY_CONFIG environment variable or proxy.istio.io/config annotation.
func getPortFromProxyConfig() (int32, error) {
	proxyConfig, err := constructProxyConfig()
	if err != nil {
		return 0, err
	}
	return proxyConfig.ProxyAdminPort, nil
}
