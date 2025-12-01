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

// An example implementation of a client.

package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	// To install the xds resolvers and balancers.
	_ "google.golang.org/grpc/xds"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/forwarder"
)

var (
	count                   int
	timeout                 time.Duration
	qps                     int
	uds                     string
	headers                 []string
	msg                     string
	expect                  string
	expectSet               bool
	method                  string
	http2                   bool
	http3                   bool
	insecureSkipVerify      bool
	alpn                    []string
	serverName              string
	serverFirst             bool
	followRedirects         bool
	newConnectionPerRequest bool
	v4Only                  bool
	v6Only                  bool
	forceDNSLookup          bool

	clientCert string
	clientKey  string

	caFile string

	hboneAddress                 string
	hboneHeaders                 []string
	doubleHboneAddress           string
	hboneClientCert              string
	hboneClientKey               string
	hboneCaFile                  string
	hboneInsecureSkipVerify      bool
	innerHboneCaFile             string
	innerHboneInsecureSkipVerify bool

	loggingOptions = log.DefaultOptions()

	rootCmd = &cobra.Command{
		Use:          "client",
		Short:        "Istio test Echo client.",
		SilenceUsage: true,
		Long: `Istio test Echo client used for generating traffic between instances of the Echo service.
For Kubernetes, this can be run from a source pod via "kubectl exec" with the desired flags.
In general, Echo's gRPC interface (ForwardEcho) should be preferred. This client is only needed in cases
where the network configuration doesn't support gRPC to the source pod.'
`,
		Args:              cobra.ExactArgs(1),
		PersistentPreRunE: configureLogging,
		Run: func(cmd *cobra.Command, args []string) {
			expectSet = cmd.Flags().Changed("expect")
			// Create a request from the flags.
			request, err := getRequest(args[0])
			if err != nil {
				log.Fatal(err)
			}

			// Create a forwarder.
			f := forwarder.New()
			defer func() {
				_ = f.Close()
			}()

			// Forward the requests.
			response, err := f.ForwardEcho(context.Background(), &forwarder.Config{
				Request: request,
				UDS:     uds,
			})
			if err != nil {
				log.Fatalf("Error %s\n", err) // nolint: revive
				os.Exit(-1)
			}

			// Log the output to stdout.
			for _, line := range response.Output {
				fmt.Println(line)
			}

			log.Infof("All %d requests succeeded", len(response.Output))
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
	rootCmd.PersistentFlags().IntVar(&count, "count", common.DefaultCount, "Number of times to make the request")
	rootCmd.PersistentFlags().IntVar(&qps, "qps", 0, "Queries per second")
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", common.DefaultRequestTimeout, "Request timeout")
	rootCmd.PersistentFlags().StringVar(&uds, "uds", "",
		"Specify the Unix Domain Socket to connect to")
	rootCmd.PersistentFlags().StringSliceVarP(&headers, "header", "H", headers,
		"A list of http headers (use Host for authority) - 'name: value', following curl syntax")
	rootCmd.PersistentFlags().StringVar(&caFile, "ca", "", "CA root cert file")
	rootCmd.PersistentFlags().StringVar(&msg, "msg", "HelloWorld",
		"message to send (for websockets)")
	rootCmd.PersistentFlags().StringVar(&expect, "expect", "",
		"message to expect (for tcp)")
	rootCmd.PersistentFlags().StringVar(&method, "method", "", "method to use (for HTTP)")
	rootCmd.PersistentFlags().BoolVar(&http2, "http2", false,
		"send http requests as HTTP2 with prior knowledge")
	rootCmd.PersistentFlags().BoolVar(&http3, "http3", false,
		"send http requests as HTTP 3")
	rootCmd.PersistentFlags().BoolVarP(&insecureSkipVerify, "insecure-skip-verify", "k", insecureSkipVerify,
		"do not verify TLS")
	rootCmd.PersistentFlags().BoolVar(&serverFirst, "server-first", false,
		"Treat as a server first protocol; do not send request until magic string is received")
	rootCmd.PersistentFlags().BoolVarP(&followRedirects, "follow-redirects", "L", false,
		"If enabled, will follow 3xx redirects with the Location header")
	rootCmd.PersistentFlags().BoolVar(&newConnectionPerRequest, "new-connection-per-request", false,
		"If enabled, a new connection will be made to the server for each individual request. "+
			"If false, an attempt will be made to re-use the connection for the life of the forward request. "+
			"This is automatically set for DNS, TCP, TLS, and WebSocket protocols.")
	rootCmd.PersistentFlags().BoolVarP(&v4Only, "ipv4", "4", false, "Only use IPv4")
	rootCmd.PersistentFlags().BoolVarP(&v6Only, "ipv6", "6", false, "Only use IPv6")
	rootCmd.PersistentFlags().BoolVar(&forceDNSLookup, "force-dns-lookup", false,
		"If enabled, each request will force a DNS lookup. Only applies if new-connection-per-request is also enabled.")
	rootCmd.PersistentFlags().StringVar(&clientCert, "client-cert", "", "client certificate file to use for request")
	rootCmd.PersistentFlags().StringVar(&clientKey, "client-key", "", "client certificate key file to use for request")
	rootCmd.PersistentFlags().StringSliceVarP(&alpn, "alpn", "", nil, "alpn to set")
	rootCmd.PersistentFlags().StringVarP(&serverName, "server-name", "", serverName, "server name to set")

	rootCmd.PersistentFlags().StringVar(&hboneAddress, "hbone", "", "address to send HBONE request to")
	rootCmd.PersistentFlags().StringVar(&doubleHboneAddress, "double-hbone", "", "address to send double HBONE request to")
	rootCmd.PersistentFlags().StringSliceVarP(&hboneHeaders, "hbone-header", "M", hboneHeaders,
		"A list of http headers for HBONE connection (use Host for authority) - 'name: value', following curl syntax")
	rootCmd.PersistentFlags().StringVar(&hboneCaFile, "hbone-ca", "", "CA root cert file used for the HBONE request")
	rootCmd.PersistentFlags().StringVar(&hboneClientCert, "hbone-client-cert", "", "client certificate file used for the HBONE request")
	rootCmd.PersistentFlags().StringVar(&hboneClientKey, "hbone-client-key", "", "client certificate key file used for the HBONE request")
	rootCmd.PersistentFlags().BoolVar(&hboneInsecureSkipVerify, "hbone-insecure-skip-verify", hboneInsecureSkipVerify, "skip TLS verification of HBONE request")
	rootCmd.PersistentFlags().StringVar(&innerHboneCaFile, "inner-hbone-ca", "",
		"CA root cert file used for the inner HBONE request. Only used if --double-hbone is set")
	rootCmd.PersistentFlags().BoolVar(&innerHboneInsecureSkipVerify, "inner-hbone-insecure-skip-verify", innerHboneInsecureSkipVerify,
		"skip TLS verification of inner HBONE request")

	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)
}

// Adds http scheme to and url if not set. This matches curl logic
func defaultScheme(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	if p.Scheme == "" {
		return "http://" + u
	}
	return u
}

func getRequest(url string) (*proto.ForwardEchoRequest, error) {
	request := &proto.ForwardEchoRequest{
		Url:                     defaultScheme(url),
		TimeoutMicros:           common.DurationToMicros(timeout),
		Count:                   int32(count),
		Qps:                     int32(qps),
		Message:                 msg,
		Http2:                   http2,
		Http3:                   http3,
		ServerFirst:             serverFirst,
		FollowRedirects:         followRedirects,
		Method:                  method,
		ServerName:              serverName,
		InsecureSkipVerify:      insecureSkipVerify,
		NewConnectionPerRequest: newConnectionPerRequest,
		ForceDNSLookup:          forceDNSLookup,
	}
	if v4Only && v6Only {
		return nil, fmt.Errorf("--v4-only and --v6-only are mutually exclusive")
	} else if v4Only {
		request.ForceIpFamily = "tcp4"
	} else if v6Only {
		request.ForceIpFamily = "tcp6"
	}
	if len(doubleHboneAddress) > 0 {
		request.DoubleHbone = &proto.HBONE{
			Address:            doubleHboneAddress,
			CertFile:           hboneClientCert,
			KeyFile:            hboneClientKey,
			CaCertFile:         hboneCaFile,
			InsecureSkipVerify: hboneInsecureSkipVerify,
		}
		for _, header := range hboneHeaders {
			parts := strings.SplitN(header, ":", 2)
			// require name:value format
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid header format: %q (want name:value)", header)
			}

			request.DoubleHbone.Headers = append(request.Hbone.Headers, &proto.Header{
				Key:   parts[0],
				Value: strings.Trim(parts[1], " "),
			})
		}

		request.Hbone = &proto.HBONE{
			CertFile:           hboneClientCert, // same creds for inner tunnel
			KeyFile:            hboneClientKey,
			CaCertFile:         hboneCaFile,
			InsecureSkipVerify: hboneInsecureSkipVerify,
		}
		if len(innerHboneCaFile) > 0 {
			request.Hbone.CaCertFile = innerHboneCaFile
		}
		if innerHboneInsecureSkipVerify != hboneInsecureSkipVerify {
			request.Hbone.InsecureSkipVerify = innerHboneInsecureSkipVerify
		}
	}
	if len(hboneAddress) > 0 {
		request.Hbone = &proto.HBONE{
			Address:            hboneAddress,
			CertFile:           hboneClientCert,
			KeyFile:            hboneClientKey,
			CaCertFile:         hboneCaFile,
			InsecureSkipVerify: hboneInsecureSkipVerify,
		}
		for _, header := range hboneHeaders {
			parts := strings.SplitN(header, ":", 2)
			// require name:value format
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid header format: %q (want name:value)", header)
			}

			request.Hbone.Headers = append(request.Hbone.Headers, &proto.Header{
				Key:   parts[0],
				Value: strings.Trim(parts[1], " "),
			})
		}
	}

	if expectSet {
		request.ExpectedResponse = &wrappers.StringValue{Value: expect}
	}

	if alpn != nil {
		request.Alpn = &proto.Alpn{Value: alpn}
	}

	for _, header := range headers {
		headerKey, headerVal, colonFound := strings.Cut(header, ":")
		if !colonFound {
			return nil, fmt.Errorf("invalid header format: %q (want name:value)", header)
		}

		request.Headers = append(request.Headers, &proto.Header{
			Key:   headerKey,
			Value: strings.TrimSpace(headerVal),
		})
	}

	if clientCert != "" && clientKey != "" {
		request.CertFile = clientCert
		request.KeyFile = clientKey
	}
	if caFile != "" {
		request.CaCertFile = caFile
	}
	return request, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
