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
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/atomic"
	_ "google.golang.org/grpc/xds" // To install the xds resolvers and balancers.

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/server"
)

var (
	httpPorts           []int
	grpcPorts           []int
	tcpPorts            []int
	udpPorts            []int
	tlsPorts            []int
	mtlsPorts           []int
	hbonePorts          []int
	doubleHbonePorts    []int
	instanceIPPorts     []int
	localhostIPPorts    []int
	serverFirstPorts    []int
	proxyProtocolPorts  []int
	xdsGRPCServers      []int
	endpointPickerPorts []int
	metricsPort         int
	uds                 string
	version             string
	cluster             string
	crt                 string
	key                 string
	ca                  string
	istioVersion        string
	disableALPN         bool

	loggingOptions = log.DefaultOptions()

	rootCmd = &cobra.Command{
		Use:               "server",
		Short:             "Echo server application.",
		SilenceUsage:      true,
		Long:              `Echo application for testing Istio E2E`,
		PersistentPreRunE: configureLogging,
		Run: func(cmd *cobra.Command, args []string) {
			shutdown := NewShutdown()
			ports := make(common.PortList, len(httpPorts)+len(grpcPorts)+len(tcpPorts)+len(udpPorts)+len(hbonePorts)+len(doubleHbonePorts)+len(endpointPickerPorts))
			tlsByPort := map[int]bool{}
			mtlsByPort := map[int]bool{}
			for _, p := range tlsPorts {
				tlsByPort[p] = true
			}
			for _, p := range mtlsPorts {
				// mTLS ports are also TLS ports.
				tlsByPort[p] = true
				mtlsByPort[p] = true
			}
			serverFirstByPort := map[int]bool{}
			for _, p := range serverFirstPorts {
				serverFirstByPort[p] = true
			}
			proxyProtocolByPort := map[int]bool{}
			for _, p := range proxyProtocolPorts {
				proxyProtocolByPort[p] = true
			}
			xdsGRPCByPort := map[int]bool{}
			for _, p := range xdsGRPCServers {
				xdsGRPCByPort[p] = true
			}
			endpointPickerByPort := map[int]bool{}
			for _, p := range endpointPickerPorts {
				endpointPickerByPort[p] = true
			}
			portIndex := 0
			for i, p := range httpPorts {
				ports[portIndex] = &common.Port{
					Name:              "http-" + strconv.Itoa(i),
					Protocol:          protocol.HTTP,
					Port:              p,
					TLS:               tlsByPort[p],
					RequireClientCert: mtlsByPort[p],
					ServerFirst:       serverFirstByPort[p],
					ProxyProtocol:     proxyProtocolByPort[p],
				}
				portIndex++
			}
			for i, p := range grpcPorts {
				ports[portIndex] = &common.Port{
					Name:          "grpc-" + strconv.Itoa(i),
					Protocol:      protocol.GRPC,
					Port:          p,
					TLS:           tlsByPort[p],
					ServerFirst:   serverFirstByPort[p],
					XDSServer:     xdsGRPCByPort[p],
					ProxyProtocol: proxyProtocolByPort[p],
				}
				portIndex++
			}
			for i, p := range tcpPorts {
				ports[portIndex] = &common.Port{
					Name:          "tcp-" + strconv.Itoa(i),
					Protocol:      protocol.TCP,
					Port:          p,
					TLS:           tlsByPort[p],
					ServerFirst:   serverFirstByPort[p],
					ProxyProtocol: proxyProtocolByPort[p],
				}
				portIndex++
			}
			for i, p := range udpPorts {
				ports[portIndex] = &common.Port{
					Name:     "udp-" + strconv.Itoa(i),
					Protocol: protocol.UDP,
					Port:     p,
				}
				portIndex++
			}
			for i, p := range hbonePorts {
				ports[portIndex] = &common.Port{
					Name:     "hbone-" + strconv.Itoa(i),
					Protocol: protocol.HBONE,
					Port:     p,
					TLS:      tlsByPort[p],
				}
				portIndex++
			}
			for i, p := range doubleHbonePorts {
				ports[portIndex] = &common.Port{
					Name:     "double-hbone-" + strconv.Itoa(i),
					Protocol: protocol.DoubleHBONE,
					Port:     p,
					TLS:      tlsByPort[p],
				}
				portIndex++
			}
			for i, p := range endpointPickerPorts {
				ports[portIndex] = &common.Port{
					Name:           "endpoint-picker-" + strconv.Itoa(i),
					Protocol:       protocol.GRPC,
					Port:           p,
					TLS:            tlsByPort[p],
					ServerFirst:    serverFirstByPort[p],
					ProxyProtocol:  proxyProtocolByPort[p],
					EndpointPicker: true,
				}
				portIndex++
			}

			instanceIPByPort := map[int]struct{}{}
			for _, p := range instanceIPPorts {
				instanceIPByPort[p] = struct{}{}
			}
			localhostIPByPort := map[int]struct{}{}
			for _, p := range localhostIPPorts {
				localhostIPByPort[p] = struct{}{}
			}

			s := server.New(server.Config{
				Ports:                 ports,
				Metrics:               metricsPort,
				BindIPPortsMap:        instanceIPByPort,
				BindLocalhostPortsMap: localhostIPByPort,
				TLSCert:               crt,
				TLSKey:                key,
				TLSCACert:             ca,
				Version:               version,
				Cluster:               cluster,
				IstioVersion:          istioVersion,
				Namespace:             os.Getenv("NAMESPACE"),
				UDSServer:             uds,
				DisableALPN:           disableALPN,
				ReportRequest:         shutdown.ReportRequest,
			})

			if err := s.Start(); err != nil {
				log.Error(err)
				os.Exit(-1)
			}
			defer func() {
				_ = s.Close()
			}()
			shutdown.WaitForShutdown()
		},
	}
)

const shutdownTime = time.Second

type Shutdown struct {
	timer *atomic.Pointer[time.Timer]
}

func NewShutdown() *Shutdown {
	return &Shutdown{timer: atomic.NewPointer[time.Timer](nil)}
}

func (s *Shutdown) ReportRequest() {
	// On every request, reset our shutdown timer. This lets us dynamically drain: if we continue to receive requests, we will
	// keep alive up to 10s. If not, we will shutdown quickly (shutdownTimer)
	if timer := s.timer.Load(); timer != nil {
		timer.Reset(shutdownTime)
	}
}

func (s *Shutdown) WaitForShutdown() {
	// Wait for the process to be shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	// Keep alive while we are still receiving requests.
	log.Infof("Draining server")
	maxShutdownTime := time.After(time.Second * 10)
	ti := time.NewTimer(shutdownTime)
	s.timer.Store(ti)
	select {
	case <-maxShutdownTime:
		log.Warnf("Shutdown after 10s while requests were still incoming")
		return
	case <-ti.C:
		log.Infof("Shutdown complete")
	case <-sigs:
		log.Infof("Shutdown forced")
	}
}

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
	rootCmd.PersistentFlags().IntSliceVar(&udpPorts, "udp", []int{}, "UDP ports")
	rootCmd.PersistentFlags().IntSliceVar(&hbonePorts, "hbone", []int{}, "HBONE ports")
	rootCmd.PersistentFlags().IntSliceVar(&doubleHbonePorts, "double-hbone", []int{}, "Double HBONE ports")
	rootCmd.PersistentFlags().IntSliceVar(&tlsPorts, "tls", []int{}, "Ports that are using TLS. These must be defined as http/grpc/tcp.")
	rootCmd.PersistentFlags().IntSliceVar(&mtlsPorts, "mtls", []int{}, "Ports that are using mTLS. These must be defined as http.")
	rootCmd.PersistentFlags().IntSliceVar(&instanceIPPorts, "bind-ip", []int{}, "Ports that are bound to INSTANCE_IP rather than wildcard IP.")
	rootCmd.PersistentFlags().IntSliceVar(&localhostIPPorts, "bind-localhost", []int{}, "Ports that are bound to localhost rather than wildcard IP.")
	rootCmd.PersistentFlags().IntSliceVar(&serverFirstPorts, "server-first", []int{}, "Ports that are server first. These must be defined as tcp.")
	rootCmd.PersistentFlags().IntSliceVar(&proxyProtocolPorts, "proxy-protocol", []int{}, "Ports that are wrapped in HA-PROXY protocol.")
	rootCmd.PersistentFlags().IntSliceVar(&xdsGRPCServers, "xds-grpc-server", []int{}, "Ports that should rely on XDS configuration to serve.")
	rootCmd.PersistentFlags().IntSliceVar(&endpointPickerPorts, "endpoint-picker", []int{},
		"Endpoint picker (ext_proc) ports. These are GRPC ports that implement the Envoy external processor protocol.")
	rootCmd.PersistentFlags().IntVar(&metricsPort, "metrics", 0, "Metrics port")
	rootCmd.PersistentFlags().StringVar(&uds, "uds", "", "HTTP server on unix domain socket")
	rootCmd.PersistentFlags().StringVar(&version, "version", "", "Version string")
	rootCmd.PersistentFlags().StringVar(&cluster, "cluster", "", "Cluster where this server is deployed")
	rootCmd.PersistentFlags().StringVar(&crt, "crt", "", "TLS server-side certificate")
	rootCmd.PersistentFlags().StringVar(&key, "key", "", "TLS server-side key")
	rootCmd.PersistentFlags().StringVar(&ca, "ca", "", "TLS CA certificate")
	rootCmd.PersistentFlags().StringVar(&istioVersion, "istio-version", "", "Istio sidecar version")
	rootCmd.PersistentFlags().BoolVar(&disableALPN, "disable-alpn", disableALPN, "disable ALPN negotiation")

	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
