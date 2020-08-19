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
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/forwarder"
	"istio.io/pkg/log"
)

var (
	count      int
	timeout    time.Duration
	qps        int
	url        string
	uds        string
	headerKey  string
	headerVal  string
	headers    string
	msg        string
	http2      bool
	clientCert string
	clientKey  string

	caFile string

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
		PersistentPreRunE: configureLogging,
		Run: func(cmd *cobra.Command, args []string) {
			// Create a request from the flags.
			request, err := getRequest()
			if err != nil {
				log.Fatala(err)
				os.Exit(-1)
			}

			// Create a forwarder.
			f, err := forwarder.New(forwarder.Config{
				Request: request,
				UDS:     uds,
				TLSCert: caFile,
			})
			if err != nil {
				log.Fatalf("Failed to create forwarder: %v", err)
				os.Exit(-1)
			}

			// Run the forwarder.
			defer func() {
				_ = f.Close()
			}()
			response, err := f.Run(context.Background())
			if err != nil {
				log.Fatalf("Error %s\n", err)
				os.Exit(-1)
			}

			// Log the output to stdout.
			for _, line := range response.Output {
				fmt.Println(line)
			}

			log.Infof("All requests succeeded")
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
	rootCmd.PersistentFlags().StringVar(&url, "url", "", "Specify URL")
	rootCmd.PersistentFlags().StringVar(&uds, "uds", "",
		"Specify the Unix Domain Socket to connect to")
	rootCmd.PersistentFlags().StringVar(&headerKey, "key", "",
		"Header key (use Host for authority) - deprecated user headers instead")
	rootCmd.PersistentFlags().StringVar(&headerVal, "val", "", "Header value - deprecated")
	rootCmd.PersistentFlags().StringVar(&headers, "headers", "",
		"A list of http headers (use Host for authority) - name:value[,name:value]*")
	rootCmd.PersistentFlags().StringVar(&caFile, "ca", "/cert.crt", "CA root cert file")
	rootCmd.PersistentFlags().StringVar(&msg, "msg", "HelloWorld",
		"message to send (for websockets)")
	rootCmd.PersistentFlags().BoolVar(&http2, "http2", false,
		"send http requests as HTTP with prior knowledge")
	rootCmd.PersistentFlags().StringVar(&clientCert, "client-cert", "", "client certificate file to use for request")
	rootCmd.PersistentFlags().StringVar(&clientKey, "client-key", "", "client certificate key file to use for request")

	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)
}

func getRequest() (*proto.ForwardEchoRequest, error) {
	request := &proto.ForwardEchoRequest{
		Url:           url,
		TimeoutMicros: common.DurationToMicros(timeout),
		Count:         int32(count),
		Qps:           int32(qps),
		Message:       msg,
		Http2:         http2,
	}

	// Old http add header - deprecated
	if headerKey != "" {
		request.Headers = append(request.Headers, &proto.Header{
			Key:   headerKey,
			Value: headerVal,
		})
	}

	if headers != "" {
		headersList := strings.Split(headers, ",")
		for _, header := range headersList {
			parts := strings.Split(header, ":")

			// require name:value format
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid header format: %q (want name:value)", header)
			}

			request.Headers = append(request.Headers, &proto.Header{
				Key:   parts[0],
				Value: parts[1],
			})
		}
	}

	if clientCert != "" && clientKey != "" {
		certData, err := ioutil.ReadFile(clientCert)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		request.Cert = string(certData)
		keyData, err := ioutil.ReadFile(clientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate key: %v", err)
		}
		request.Key = string(keyData)
	}
	return request, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
