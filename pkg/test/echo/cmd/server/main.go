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
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/server"
	"istio.io/pkg/log"
)

var (
	httpPorts        []int
	grpcPorts        []int
	tcpPorts         []int
	tlsPorts         []int
	serverFirstPorts []int
	metricsPort      int
	uds              string
	version          string
	cluster          string
	crt              string
	key              string

	loggingOptions = log.DefaultOptions()

	rootCmd = &cobra.Command{
		Use:               "server",
		Short:             "Echo server application.",
		SilenceUsage:      true,
		Long:              `Echo application for testing Istio E2E`,
		PersistentPreRunE: configureLogging,
		Run: func(cmd *cobra.Command, args []string) {
			// check for duplicate ports in lists
			errMsg := checkForDuplicatePorts()

			if len(errMsg) > 0 {
				var errorStr string
				for _, val := range errMsg {
					errorStr = errorStr + fmt.Sprintf("\n%s", val)

				}
				log.Errora(errorStr)
				os.Exit(-1)
			}
			ports := make(common.PortList, len(httpPorts)+len(grpcPorts)+len(tcpPorts))
			tlsByPort := map[int]bool{}
			for _, p := range tlsPorts {
				tlsByPort[p] = true
			}
			serverFirstByPort := map[int]bool{}
			for _, p := range serverFirstPorts {
				serverFirstByPort[p] = true
			}
			portIndex := 0
			for i, p := range httpPorts {
				ports[portIndex] = &common.Port{
					Name:        "http-" + strconv.Itoa(i),
					Protocol:    protocol.HTTP,
					Port:        p,
					TLS:         tlsByPort[p],
					ServerFirst: serverFirstByPort[p],
				}
				portIndex++
			}
			for i, p := range grpcPorts {
				ports[portIndex] = &common.Port{
					Name:        "grpc-" + strconv.Itoa(i),
					Protocol:    protocol.GRPC,
					Port:        p,
					TLS:         tlsByPort[p],
					ServerFirst: serverFirstByPort[p],
				}
				portIndex++
			}
			for i, p := range tcpPorts {
				ports[portIndex] = &common.Port{
					Name:        "tcp-" + strconv.Itoa(i),
					Protocol:    protocol.TCP,
					Port:        p,
					TLS:         tlsByPort[p],
					ServerFirst: serverFirstByPort[p],
				}
				portIndex++
			}

			s := server.New(server.Config{
				Ports:     ports,
				Metrics:   metricsPort,
				TLSCert:   crt,
				TLSKey:    key,
				Version:   version,
				Cluster:   cluster,
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
	rootCmd.PersistentFlags().IntSliceVar(&tcpPorts, "tcp", []int{9090}, "TCP ports")
	rootCmd.PersistentFlags().IntSliceVar(&tlsPorts, "tls", []int{}, "Ports that are using TLS. These must be defined as http/grpc/tcp.")
	rootCmd.PersistentFlags().IntSliceVar(&serverFirstPorts, "server-first", []int{}, "Ports that are server first. These must be defined as tcp.")
	rootCmd.PersistentFlags().IntVar(&metricsPort, "metrics", 0, "Metrics port")
	rootCmd.PersistentFlags().StringVar(&uds, "uds", "", "HTTP server on unix domain socket")
	rootCmd.PersistentFlags().StringVar(&version, "version", "", "Version string")
	rootCmd.PersistentFlags().StringVar(&cluster, "cluster", "", "Cluster where this server is deployed")
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

func checkForDuplicatePorts() []string {
	strSlc := make([]string, 0)
	strSlc = append(strSlc, checkItself(httpPorts, "httpPorts")...)
	strSlc = append(strSlc, checkItself(grpcPorts, "grpcPorts")...)
	strSlc = append(strSlc, checkItself(tcpPorts, "tcpPorts")...)

	for i := 0; i < len(httpPorts); i++ {
		for j := 0; j < len(tcpPorts); j++ {
			if httpPorts[i] == tcpPorts[j] {
				s := fmt.Sprintf("Duplicate Port http:%d and tcp:%d", httpPorts[i], tcpPorts[j])
				strSlc = append(strSlc, s)
			}
			if httpPorts[i] == grpcPorts[j] {
				s := fmt.Sprintf("Duplicate Port http:%d and grpc:%d", httpPorts[i], grpcPorts[j])
				strSlc = append(strSlc, s)
			}
		}

	}
	for i := 0; i < len(tcpPorts); i++ {
		for j := 0; j < len(grpcPorts); j++ {
			if tcpPorts[i] == grpcPorts[j] {
				s := fmt.Sprintf("Duplicate Port tcp:%d and grpc:%d", tcpPorts[i], grpcPorts[j])
				strSlc = append(strSlc, s)
			}
		}
	}

	return strSlc
}

func checkItself(arr []int, portType string) []string {
	slc := make([]string, 0)
	for i := 0; i < len(arr); i++ {
		for j := i + 1; j <= len(arr)-1; j++ {
			if arr[i] == arr[j] {
				slc = append(slc, fmt.Sprintf("Duplicate port in %s", portType))
			}
		}
	}
	return slc
}
