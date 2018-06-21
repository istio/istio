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

package cmd

import (
	"context"
	"net"
	"time"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	mpb "github.com/envoyproxy/go-control-plane/envoy/service/metrics/v2"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	prometheus "istio.io/gogo-genproto/prometheus"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/direct/envoymetrics"
)

func metricsServiceRoot(rootArgs *RootArgs, printf, fatalf shared.FormatFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Envoy Metrics Service command",
	}

	cmd.AddCommand(metricsServiceServer(rootArgs, printf, fatalf), metricsServiceClient(rootArgs, printf, fatalf))
	return cmd
}

func metricsServiceServer(rootArgs *RootArgs, printf, fatalf shared.FormatFn) *cobra.Command {

	var batchSize int64
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start a Metrics Service Server",
		Run: func(cmd *cobra.Command, args []string) {
			grpcServer := grpc.NewServer()

			// TODO: batch size
			srv, err := envoymetrics.NewMetricsServiceServer(rootArgs.mixerAddress, batchSize)
			if err != nil {
				fatalf("could not build server: %v", err)
			}

			mpb.RegisterMetricsServiceServer(grpcServer, srv)

			l, err := net.Listen("tcp", ":6000")
			if err != nil {
				fatalf("failed to establish listener: %v", err)
			}

			printf("listening on tcp://localhost:6000")
			grpcServer.Serve(l)

		},
	}

	cmd.PersistentFlags().Int64VarP(&batchSize, "batch", "b", 1000, "Size of batch to report")

	return cmd
}

func metricsServiceClient(rootArgs *RootArgs, printf, fatalf shared.FormatFn) *cobra.Command {

	cmd := &cobra.Command{
		Use:   "client",
		Short: "Start a test Metrics Service client",
		Run: func(cmd *cobra.Command, args []string) {
			conn, err := grpc.Dial("localhost:6000", grpc.WithInsecure())
			if err != nil {
				fatalf("failed to connect: %s", err)
			}
			defer conn.Close()
			client := mpb.NewMetricsServiceClient(conn)
			stream, err := client.StreamMetrics(context.Background())
			waitc := make(chan struct{})
			go func() {
				for {
					time.Sleep(100 * time.Millisecond)
					stream.Send(message())
				}
			}()
			<-waitc
			stream.CloseSend()
		},
	}

	return cmd
}

func message() *mpb.StreamMetricsMessage {
	return &mpb.StreamMetricsMessage{
		Identifier: &mpb.StreamMetricsMessage_Identifier{
			Node: &corepb.Node{
				BuildVersion: "test",
				Cluster:      "mycluster",
				Id:           "sidecar~10.0.2.51~details-v1-7dbc79ffc-98gt8.default~default.svc.cluster.local",
				Locality: &corepb.Locality{
					Region: "region",
					Zone:   "testzone",
				},
			},
		},
		EnvoyMetrics: []*prometheus.MetricFamily{
			&prometheus.MetricFamily{
				Name: "family-one",
				Type: prometheus.MetricType_GAUGE,
				Metric: []*prometheus.Metric{
					&prometheus.Metric{
						TimestampMs: time.Now().UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond)),
						Label: []*prometheus.LabelPair{
							&prometheus.LabelPair{Name: "testing", Value: "labelsVal"},
						},
						Gauge: &prometheus.Gauge{Value: 234.245345},
					},
				},
			},
		},
	}
}
