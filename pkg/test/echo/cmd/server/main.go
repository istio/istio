// Copyright 2017 Istio Authors
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
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/server"
)

var (
	httpPorts []int
	grpcPorts []int
	uds       string
	version   string
	crt       string
	key       string

	loggingOptions = log.DefaultOptions()

	rootCmd = &cobra.Command{
		Use:               "server",
		Short:             "Echo server application.",
		SilenceUsage:      true,
		Long:              `Echo application for testing Istio E2E`,
		PersistentPreRunE: configureLogging,
		Run: func(cmd *cobra.Command, args []string) {
			ports := make(model.PortList, len(httpPorts)+len(grpcPorts))
			portIndex := 0
			for i, p := range httpPorts {
				ports[portIndex] = &model.Port{
					Name:     "http-" + strconv.Itoa(i),
					Protocol: model.ProtocolHTTP,
					Port:     p,
				}
				portIndex++
			}
			for i, p := range grpcPorts {
				ports[portIndex] = &model.Port{
					Name:     "grpc-" + strconv.Itoa(i),
					Protocol: model.ProtocolGRPC,
					Port:     p,
				}
				portIndex++
			}

			s := server.New(server.Config{
				Ports:     ports,
				TLSCert:   crt,
				TLSKey:    key,
				Version:   version,
				UDSServer: uds,
			})

			if err := s.Start(); err != nil {
				log.Errora(err)
				os.Exit(-1)
			}
			defer func() {
				_ = s.Close()
			}()

			// Wait for the process to be shutdown.
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			<-sigs
		},
	}
)

func configureLogging(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().IntSliceVar(&httpPorts, "port", []int{8080}, "HTTP/1.1 ports")
	rootCmd.PersistentFlags().IntSliceVar(&grpcPorts, "grpc", []int{7070}, "GRPC ports")
	rootCmd.PersistentFlags().StringVar(&uds, "uds", "", "HTTP server on unix domain socket")
	rootCmd.PersistentFlags().StringVar(&version, "version", "", "Version string")
	rootCmd.PersistentFlags().StringVar(&crt, "crt", "", "gRPC TLS server-side certificate")
	rootCmd.PersistentFlags().StringVar(&key, "key", "", "gRPC TLS server-side key")

	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
