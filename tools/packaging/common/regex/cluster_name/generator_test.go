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
	"log"
	"regexp"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestClusterNameRegex(t *testing.T) {
	regexList, err := ReadClusterNameRegex(metricsFileName)
	if err != nil {
		log.Fatal(err)
	}
	regexString := BuildClusterNameRegex(regexList)

	regex, err := regexp.Compile(regexString)
	if err != nil {
		log.Fatal(err)
	}

	type fields struct {
		input string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name:   "kubernetes service match upstream_cx_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "external service match upstream_cx_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "external service with subset match upstream_cx_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "kubernetes service match upstream_cx_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "external service match upstream_cx_active",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "external service with subset match upstream_cx_active",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "kubernetes service match upstream_cx_http1_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_http1_total"},
			want:   ".upstream_cx_http1_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_http1_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_http1_total"},
			want:   ".upstream_cx_http1_total",
		},
		{
			name:   "external service match upstream_cx_http1_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_http1_total"},
			want:   ".upstream_cx_http1_total",
		},
		{
			name:   "external service with subset match upstream_cx_http1_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_http1_total"},
			want:   ".upstream_cx_http1_total",
		},
		{
			name:   "kubernetes service match upstream_cx_http2_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_http2_total"},
			want:   ".upstream_cx_http2_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_http2_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_http2_total"},
			want:   ".upstream_cx_http2_total",
		},
		{
			name:   "external service match upstream_cx_http2_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_http2_total"},
			want:   ".upstream_cx_http2_total",
		},
		{
			name:   "external service with subset match upstream_cx_http2_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_http2_total"},
			want:   ".upstream_cx_http2_total",
		},
		{
			name:   "kubernetes service match upstream_cx_http3_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_http3_total"},
			want:   ".upstream_cx_http3_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_http3_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_http3_total"},
			want:   ".upstream_cx_http3_total",
		},
		{
			name:   "external service match upstream_cx_http3_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_http3_total"},
			want:   ".upstream_cx_http3_total",
		},
		{
			name:   "external service with subset match upstream_cx_http3_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_http3_total"},
			want:   ".upstream_cx_http3_total",
		},
		{
			name:   "kubernetes service match upstream_cx_connect_fail",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_connect_fail"},
			want:   ".upstream_cx_connect_fail",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_connect_fail",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_connect_fail"},
			want:   ".upstream_cx_connect_fail",
		},
		{
			name:   "external service match upstream_cx_connect_fail",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_connect_fail"},
			want:   ".upstream_cx_connect_fail",
		},
		{
			name:   "external service with subset match upstream_cx_connect_fail",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_connect_fail"},
			want:   ".upstream_cx_connect_fail",
		},
		{
			name:   "kubernetes service match upstream_cx_connect_timeout",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_connect_timeout"},
			want:   ".upstream_cx_connect_timeout",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_connect_timeout",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_connect_timeout"},
			want:   ".upstream_cx_connect_timeout",
		},
		{
			name:   "external service match upstream_cx_connect_timeout",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_connect_timeout"},
			want:   ".upstream_cx_connect_timeout",
		},
		{
			name:   "external service with subset match upstream_cx_connect_timeout",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_connect_timeout"},
			want:   ".upstream_cx_connect_timeout",
		},
		{
			name:   "kubernetes service match upstream_cx_connect_with_0_rtt",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_connect_with_0_rtt"},
			want:   ".upstream_cx_connect_with_0_rtt",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_connect_with_0_rtt",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_connect_with_0_rtt"},
			want:   ".upstream_cx_connect_with_0_rtt",
		},
		{
			name:   "external service match upstream_cx_connect_with_0_rtt",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_connect_with_0_rtt"},
			want:   ".upstream_cx_connect_with_0_rtt",
		},
		{
			name:   "external service with subset match upstream_cx_connect_with_0_rtt",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_connect_with_0_rtt"},
			want:   ".upstream_cx_connect_with_0_rtt",
		},
		{
			name:   "kubernetes service match upstream_cx_idle_timeout",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_idle_timeout"},
			want:   ".upstream_cx_idle_timeout",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_idle_timeout",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_idle_timeout"},
			want:   ".upstream_cx_idle_timeout",
		},
		{
			name:   "external service match upstream_cx_idle_timeout",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_idle_timeout"},
			want:   ".upstream_cx_idle_timeout",
		},
		{
			name:   "external service with subset match upstream_cx_idle_timeout",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_idle_timeout"},
			want:   ".upstream_cx_idle_timeout",
		},
		{
			name:   "kubernetes service match upstream_cx_max_duration_reached",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_max_duration_reached"},
			want:   ".upstream_cx_max_duration_reached",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_max_duration_reached",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_max_duration_reached"},
			want:   ".upstream_cx_max_duration_reached",
		},
		{
			name:   "external service match upstream_cx_max_duration_reached",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_max_duration_reached"},
			want:   ".upstream_cx_max_duration_reached",
		},
		{
			name:   "external service with subset match upstream_cx_max_duration_reached",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_max_duration_reached"},
			want:   ".upstream_cx_max_duration_reached",
		},
		{
			name:   "kubernetes service match upstream_cx_connect_attempts_exceeded",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_connect_attempts_exceeded"},
			want:   ".upstream_cx_connect_attempts_exceeded",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_connect_attempts_exceeded",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_connect_attempts_exceeded"},
			want:   ".upstream_cx_connect_attempts_exceeded",
		},
		{
			name:   "external service match upstream_cx_connect_attempts_exceeded",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_connect_attempts_exceeded"},
			want:   ".upstream_cx_connect_attempts_exceeded",
		},
		{
			name:   "external service with subset match upstream_cx_connect_attempts_exceeded",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_connect_attempts_exceeded"},
			want:   ".upstream_cx_connect_attempts_exceeded",
		},
		{
			name:   "kubernetes service match upstream_cx_overflow",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_overflow"},
			want:   ".upstream_cx_overflow",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_overflow",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_overflow"},
			want:   ".upstream_cx_overflow",
		},
		{
			name:   "external service match upstream_cx_overflow",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_overflow"},
			want:   ".upstream_cx_overflow",
		},
		{
			name:   "external service with subset match upstream_cx_overflow",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_overflow"},
			want:   ".upstream_cx_overflow",
		},
		{
			name:   "kubernetes service match upstream_cx_connect_ms",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_connect_ms"},
			want:   ".upstream_cx_connect_ms",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_connect_ms",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_connect_ms"},
			want:   ".upstream_cx_connect_ms",
		},
		{
			name:   "external service match upstream_cx_connect_ms",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_connect_ms"},
			want:   ".upstream_cx_connect_ms",
		},
		{
			name:   "external service with subset match upstream_cx_connect_ms",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_connect_ms"},
			want:   ".upstream_cx_connect_ms",
		},
		{
			name:   "kubernetes service match upstream_cx_length_ms",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_length_ms"},
			want:   ".upstream_cx_length_ms",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_length_ms",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_length_ms"},
			want:   ".upstream_cx_length_ms",
		},
		{
			name:   "external service match upstream_cx_length_ms",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_length_ms"},
			want:   ".upstream_cx_length_ms",
		},
		{
			name:   "external service with subset match upstream_cx_length_ms",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_length_ms"},
			want:   ".upstream_cx_length_ms",
		},
		{
			name:   "kubernetes service match upstream_cx_destroy",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_destroy"},
			want:   ".upstream_cx_destroy",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_destroy",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_destroy"},
			want:   ".upstream_cx_destroy",
		},
		{
			name:   "external service match upstream_cx_destroy",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_destroy"},
			want:   ".upstream_cx_destroy",
		},
		{
			name:   "external service with subset match upstream_cx_destroy",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_destroy"},
			want:   ".upstream_cx_destroy",
		},
		{
			name:   "kubernetes service match upstream_cx_destroy_local",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_destroy_local"},
			want:   ".upstream_cx_destroy_local",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_destroy_local",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_destroy_local"},
			want:   ".upstream_cx_destroy_local",
		},
		{
			name:   "external service match upstream_cx_destroy_local",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_destroy_local"},
			want:   ".upstream_cx_destroy_local",
		},
		{
			name:   "external service with subset match upstream_cx_destroy_local",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_destroy_local"},
			want:   ".upstream_cx_destroy_local",
		},
		{
			name:   "kubernetes service match upstream_cx_destroy_remote",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_destroy_remote"},
			want:   ".upstream_cx_destroy_remote",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_destroy_remote",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_destroy_remote"},
			want:   ".upstream_cx_destroy_remote",
		},
		{
			name:   "external service match upstream_cx_destroy_remote",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_destroy_remote"},
			want:   ".upstream_cx_destroy_remote",
		},
		{
			name:   "external service with subset match upstream_cx_destroy_remote",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_destroy_remote"},
			want:   ".upstream_cx_destroy_remote",
		},
		{
			name:   "kubernetes service match upstream_cx_destroy_with_active_rq",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_destroy_with_active_rq"},
			want:   ".upstream_cx_destroy_with_active_rq",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_destroy_with_active_rq",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_destroy_with_active_rq"},
			want:   ".upstream_cx_destroy_with_active_rq",
		},
		{
			name:   "external service match upstream_cx_destroy_with_active_rq",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_destroy_with_active_rq"},
			want:   ".upstream_cx_destroy_with_active_rq",
		},
		{
			name:   "external service with subset match upstream_cx_destroy_with_active_rq",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_destroy_with_active_rq"},
			want:   ".upstream_cx_destroy_with_active_rq",
		},
		{
			name:   "kubernetes service match upstream_cx_destroy_local_with_active_rq",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_destroy_local_with_active_rq"},
			want:   ".upstream_cx_destroy_local_with_active_rq",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_destroy_local_with_active_rq",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_destroy_local_with_active_rq"},
			want:   ".upstream_cx_destroy_local_with_active_rq",
		},
		{
			name:   "external service match upstream_cx_destroy_local_with_active_rq",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_destroy_local_with_active_rq"},
			want:   ".upstream_cx_destroy_local_with_active_rq",
		},
		{
			name:   "external service with subset match upstream_cx_destroy_local_with_active_rq",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_destroy_local_with_active_rq"},
			want:   ".upstream_cx_destroy_local_with_active_rq",
		},
		{
			name:   "kubernetes service match upstream_cx_destroy_remote_with_active_rq",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_destroy_remote_with_active_rq"},
			want:   ".upstream_cx_destroy_remote_with_active_rq",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_destroy_remote_with_active_rq",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_destroy_remote_with_active_rq"},
			want:   ".upstream_cx_destroy_remote_with_active_rq",
		},
		{
			name:   "external service match upstream_cx_destroy_remote_with_active_rq",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_destroy_remote_with_active_rq"},
			want:   ".upstream_cx_destroy_remote_with_active_rq",
		},
		{
			name:   "external service with subset match upstream_cx_destroy_remote_with_active_rq",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_destroy_remote_with_active_rq"},
			want:   ".upstream_cx_destroy_remote_with_active_rq",
		},
		{
			name:   "kubernetes service match upstream_cx_close_notify",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_close_notify"},
			want:   ".upstream_cx_close_notify",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_close_notify",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_close_notify"},
			want:   ".upstream_cx_close_notify",
		},
		{
			name:   "external service match upstream_cx_close_notify",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_close_notify"},
			want:   ".upstream_cx_close_notify",
		},
		{
			name:   "external service with subset match upstream_cx_close_notify",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_close_notify"},
			want:   ".upstream_cx_close_notify",
		},
		{
			name:   "kubernetes service match upstream_cx_rx_bytes_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_rx_bytes_total"},
			want:   ".upstream_cx_rx_bytes_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_rx_bytes_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_rx_bytes_total"},
			want:   ".upstream_cx_rx_bytes_total",
		},
		{
			name:   "external service match upstream_cx_rx_bytes_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_rx_bytes_total"},
			want:   ".upstream_cx_rx_bytes_total",
		},
		{
			name:   "external service with subset match upstream_cx_rx_bytes_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_rx_bytes_total"},
			want:   ".upstream_cx_rx_bytes_total",
		},
		{
			name:   "kubernetes service match upstream_cx_rx_bytes_buffered",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_rx_bytes_buffered"},
			want:   ".upstream_cx_rx_bytes_buffered",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_rx_bytes_buffered",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_rx_bytes_buffered"},
			want:   ".upstream_cx_rx_bytes_buffered",
		},
		{
			name:   "external service match upstream_cx_rx_bytes_buffered",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_rx_bytes_buffered"},
			want:   ".upstream_cx_rx_bytes_buffered",
		},
		{
			name:   "external service with subset match upstream_cx_rx_bytes_buffered",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_rx_bytes_buffered"},
			want:   ".upstream_cx_rx_bytes_buffered",
		},
		{
			name:   "kubernetes service match upstream_cx_tx_bytes_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_tx_bytes_total"},
			want:   ".upstream_cx_tx_bytes_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_tx_bytes_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_tx_bytes_total"},
			want:   ".upstream_cx_tx_bytes_total",
		},
		{
			name:   "external service match upstream_cx_tx_bytes_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_tx_bytes_total"},
			want:   ".upstream_cx_tx_bytes_total",
		},
		{
			name:   "external service with subset match upstream_cx_tx_bytes_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_tx_bytes_total"},
			want:   ".upstream_cx_tx_bytes_total",
		},
		{
			name:   "kubernetes service match upstream_cx_tx_bytes_buffered",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_tx_bytes_buffered"},
			want:   ".upstream_cx_tx_bytes_buffered",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_tx_bytes_buffered",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_tx_bytes_buffered"},
			want:   ".upstream_cx_tx_bytes_buffered",
		},
		{
			name:   "external service match upstream_cx_tx_bytes_buffered",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_tx_bytes_buffered"},
			want:   ".upstream_cx_tx_bytes_buffered",
		},
		{
			name:   "external service with subset match upstream_cx_tx_bytes_buffered",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_tx_bytes_buffered"},
			want:   ".upstream_cx_tx_bytes_buffered",
		},
		{
			name:   "kubernetes service match upstream_cx_pool_overflow",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_pool_overflow"},
			want:   ".upstream_cx_pool_overflow",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_pool_overflow",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_pool_overflow"},
			want:   ".upstream_cx_pool_overflow",
		},
		{
			name:   "external service match upstream_cx_pool_overflow",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_pool_overflow"},
			want:   ".upstream_cx_pool_overflow",
		},
		{
			name:   "external service with subset match upstream_cx_pool_overflow",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_pool_overflow"},
			want:   ".upstream_cx_pool_overflow",
		},
		{
			name:   "kubernetes service match upstream_cx_protocol_error",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_protocol_error"},
			want:   ".upstream_cx_protocol_error",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_protocol_error",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_protocol_error"},
			want:   ".upstream_cx_protocol_error",
		},
		{
			name:   "external service match upstream_cx_protocol_error",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_protocol_error"},
			want:   ".upstream_cx_protocol_error",
		},
		{
			name:   "external service with subset match upstream_cx_protocol_error",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_protocol_error"},
			want:   ".upstream_cx_protocol_error",
		},
		{
			name:   "kubernetes service match upstream_cx_max_requests",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_max_requests"},
			want:   ".upstream_cx_max_requests",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_max_requests",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_max_requests"},
			want:   ".upstream_cx_max_requests",
		},
		{
			name:   "external service match upstream_cx_max_requests",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_max_requests"},
			want:   ".upstream_cx_max_requests",
		},
		{
			name:   "external service with subset match upstream_cx_max_requests",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_max_requests"},
			want:   ".upstream_cx_max_requests",
		},
		{
			name:   "kubernetes service match upstream_cx_none_healthy",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_cx_none_healthy"},
			want:   ".upstream_cx_none_healthy",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_none_healthy",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_cx_none_healthy"},
			want:   ".upstream_cx_none_healthy",
		},
		{
			name:   "external service match upstream_cx_none_healthy",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_none_healthy"},
			want:   ".upstream_cx_none_healthy",
		},
		{
			name:   "external service with subset match upstream_cx_none_healthy",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_cx_none_healthy"},
			want:   ".upstream_cx_none_healthy",
		},
		{
			name:   "kubernetes service match upstream_rq_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_total"},
			want:   ".upstream_rq_total",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_total"},
			want:   ".upstream_rq_total",
		},
		{
			name:   "external service match upstream_rq_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_total"},
			want:   ".upstream_rq_total",
		},
		{
			name:   "external service with subset match upstream_rq_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_total"},
			want:   ".upstream_rq_total",
		},
		{
			name:   "kubernetes service match upstream_rq_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_active"},
			want:   ".upstream_rq_active",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_active"},
			want:   ".upstream_rq_active",
		},
		{
			name:   "external service match upstream_rq_active",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_active"},
			want:   ".upstream_rq_active",
		},
		{
			name:   "external service with subset match upstream_rq_active",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_active"},
			want:   ".upstream_rq_active",
		},
		{
			name:   "kubernetes service match upstream_rq_pending_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_pending_total"},
			want:   ".upstream_rq_pending_total",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_pending_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_pending_total"},
			want:   ".upstream_rq_pending_total",
		},
		{
			name:   "external service match upstream_rq_pending_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_pending_total"},
			want:   ".upstream_rq_pending_total",
		},
		{
			name:   "external service with subset match upstream_rq_pending_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_pending_total"},
			want:   ".upstream_rq_pending_total",
		},
		{
			name:   "kubernetes service match upstream_rq_pending_overflow",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_pending_overflow"},
			want:   ".upstream_rq_pending_overflow",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_pending_overflow",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_pending_overflow"},
			want:   ".upstream_rq_pending_overflow",
		},
		{
			name:   "external service match upstream_rq_pending_overflow",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_pending_overflow"},
			want:   ".upstream_rq_pending_overflow",
		},
		{
			name:   "external service with subset match upstream_rq_pending_overflow",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_pending_overflow"},
			want:   ".upstream_rq_pending_overflow",
		},
		{
			name:   "kubernetes service match upstream_rq_pending_failure_eject",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_pending_failure_eject"},
			want:   ".upstream_rq_pending_failure_eject",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_pending_failure_eject",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_pending_failure_eject"},
			want:   ".upstream_rq_pending_failure_eject",
		},
		{
			name:   "external service match upstream_rq_pending_failure_eject",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_pending_failure_eject"},
			want:   ".upstream_rq_pending_failure_eject",
		},
		{
			name:   "external service with subset match upstream_rq_pending_failure_eject",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_pending_failure_eject"},
			want:   ".upstream_rq_pending_failure_eject",
		},
		{
			name:   "kubernetes service match upstream_rq_pending_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_pending_active"},
			want:   ".upstream_rq_pending_active",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_pending_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_pending_active"},
			want:   ".upstream_rq_pending_active",
		},
		{
			name:   "external service match upstream_rq_pending_active",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_pending_active"},
			want:   ".upstream_rq_pending_active",
		},
		{
			name:   "external service with subset match upstream_rq_pending_active",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_pending_active"},
			want:   ".upstream_rq_pending_active",
		},
		{
			name:   "kubernetes service match upstream_rq_cancelled",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_cancelled"},
			want:   ".upstream_rq_cancelled",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_cancelled",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_cancelled"},
			want:   ".upstream_rq_cancelled",
		},
		{
			name:   "external service match upstream_rq_cancelled",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_cancelled"},
			want:   ".upstream_rq_cancelled",
		},
		{
			name:   "external service with subset match upstream_rq_cancelled",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_cancelled"},
			want:   ".upstream_rq_cancelled",
		},
		{
			name:   "kubernetes service match upstream_rq_maintenance_mode",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_maintenance_mode"},
			want:   ".upstream_rq_maintenance_mode",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_maintenance_mode",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_maintenance_mode"},
			want:   ".upstream_rq_maintenance_mode",
		},
		{
			name:   "external service match upstream_rq_maintenance_mode",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_maintenance_mode"},
			want:   ".upstream_rq_maintenance_mode",
		},
		{
			name:   "external service with subset match upstream_rq_maintenance_mode",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_maintenance_mode"},
			want:   ".upstream_rq_maintenance_mode",
		},
		{
			name:   "kubernetes service match upstream_rq_timeout",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_timeout"},
			want:   ".upstream_rq_timeout",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_timeout",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_timeout"},
			want:   ".upstream_rq_timeout",
		},
		{
			name:   "external service match upstream_rq_timeout",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_timeout"},
			want:   ".upstream_rq_timeout",
		},
		{
			name:   "external service with subset match upstream_rq_timeout",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_timeout"},
			want:   ".upstream_rq_timeout",
		},
		{
			name:   "kubernetes service match upstream_rq_max_duration_reached",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_max_duration_reached"},
			want:   ".upstream_rq_max_duration_reached",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_max_duration_reached",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_max_duration_reached"},
			want:   ".upstream_rq_max_duration_reached",
		},
		{
			name:   "external service match upstream_rq_max_duration_reached",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_max_duration_reached"},
			want:   ".upstream_rq_max_duration_reached",
		},
		{
			name:   "external service with subset match upstream_rq_max_duration_reached",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_max_duration_reached"},
			want:   ".upstream_rq_max_duration_reached",
		},
		{
			name:   "kubernetes service match upstream_rq_per_try_timeout",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_per_try_timeout"},
			want:   ".upstream_rq_per_try_timeout",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_per_try_timeout",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_per_try_timeout"},
			want:   ".upstream_rq_per_try_timeout",
		},
		{
			name:   "external service match upstream_rq_per_try_timeout",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_per_try_timeout"},
			want:   ".upstream_rq_per_try_timeout",
		},
		{
			name:   "external service with subset match upstream_rq_per_try_timeout",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_per_try_timeout"},
			want:   ".upstream_rq_per_try_timeout",
		},
		{
			name:   "kubernetes service match upstream_rq_rx_reset",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_rx_reset"},
			want:   ".upstream_rq_rx_reset",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_rx_reset",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_rx_reset"},
			want:   ".upstream_rq_rx_reset",
		},
		{
			name:   "external service match upstream_rq_rx_reset",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_rx_reset"},
			want:   ".upstream_rq_rx_reset",
		},
		{
			name:   "external service with subset match upstream_rq_rx_reset",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_rx_reset"},
			want:   ".upstream_rq_rx_reset",
		},
		{
			name:   "kubernetes service match upstream_rq_tx_reset",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_tx_reset"},
			want:   ".upstream_rq_tx_reset",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_tx_reset",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_tx_reset"},
			want:   ".upstream_rq_tx_reset",
		},
		{
			name:   "external service match upstream_rq_tx_reset",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_tx_reset"},
			want:   ".upstream_rq_tx_reset",
		},
		{
			name:   "external service with subset match upstream_rq_tx_reset",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_tx_reset"},
			want:   ".upstream_rq_tx_reset",
		},
		{
			name:   "kubernetes service match upstream_rq_retry",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_retry"},
			want:   ".upstream_rq_retry",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_retry",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_retry"},
			want:   ".upstream_rq_retry",
		},
		{
			name:   "external service match upstream_rq_retry",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_retry"},
			want:   ".upstream_rq_retry",
		},
		{
			name:   "external service with subset match upstream_rq_retry",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_retry"},
			want:   ".upstream_rq_retry",
		},
		{
			name:   "kubernetes service match upstream_rq_retry_backoff_exponential",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_retry_backoff_exponential"},
			want:   ".upstream_rq_retry_backoff_exponential",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_retry_backoff_exponential",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_retry_backoff_exponential"},
			want:   ".upstream_rq_retry_backoff_exponential",
		},
		{
			name:   "external service match upstream_rq_retry_backoff_exponential",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_retry_backoff_exponential"},
			want:   ".upstream_rq_retry_backoff_exponential",
		},
		{
			name:   "external service with subset match upstream_rq_retry_backoff_exponential",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_retry_backoff_exponential"},
			want:   ".upstream_rq_retry_backoff_exponential",
		},
		{
			name:   "kubernetes service match upstream_rq_retry_backoff_ratelimited",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_retry_backoff_ratelimited"},
			want:   ".upstream_rq_retry_backoff_ratelimited",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_retry_backoff_ratelimited",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_retry_backoff_ratelimited"},
			want:   ".upstream_rq_retry_backoff_ratelimited",
		},
		{
			name:   "external service match upstream_rq_retry_backoff_ratelimited",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_retry_backoff_ratelimited"},
			want:   ".upstream_rq_retry_backoff_ratelimited",
		},
		{
			name:   "external service with subset match upstream_rq_retry_backoff_ratelimited",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_retry_backoff_ratelimited"},
			want:   ".upstream_rq_retry_backoff_ratelimited",
		},
		{
			name:   "kubernetes service match upstream_rq_retry_limit_exceeded",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_retry_limit_exceeded"},
			want:   ".upstream_rq_retry_limit_exceeded",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_retry_limit_exceeded",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_retry_limit_exceeded"},
			want:   ".upstream_rq_retry_limit_exceeded",
		},
		{
			name:   "external service match upstream_rq_retry_limit_exceeded",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_retry_limit_exceeded"},
			want:   ".upstream_rq_retry_limit_exceeded",
		},
		{
			name:   "external service with subset match upstream_rq_retry_limit_exceeded",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_retry_limit_exceeded"},
			want:   ".upstream_rq_retry_limit_exceeded",
		},
		{
			name:   "kubernetes service match upstream_rq_retry_success",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_retry_success"},
			want:   ".upstream_rq_retry_success",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_retry_success",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_retry_success"},
			want:   ".upstream_rq_retry_success",
		},
		{
			name:   "external service match upstream_rq_retry_success",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_retry_success"},
			want:   ".upstream_rq_retry_success",
		},
		{
			name:   "external service with subset match upstream_rq_retry_success",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_retry_success"},
			want:   ".upstream_rq_retry_success",
		},
		{
			name:   "kubernetes service match upstream_rq_retry_overflow",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_retry_overflow"},
			want:   ".upstream_rq_retry_overflow",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_retry_overflow",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_retry_overflow"},
			want:   ".upstream_rq_retry_overflow",
		},
		{
			name:   "external service match upstream_rq_retry_overflow",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_retry_overflow"},
			want:   ".upstream_rq_retry_overflow",
		},
		{
			name:   "external service with subset match upstream_rq_retry_overflow",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_retry_overflow"},
			want:   ".upstream_rq_retry_overflow",
		},
		{
			name:   "kubernetes service match upstream_flow_control_paused_reading_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_flow_control_paused_reading_total"},
			want:   ".upstream_flow_control_paused_reading_total",
		},
		{
			name:   "kubernetes service with subset match upstream_flow_control_paused_reading_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_flow_control_paused_reading_total"},
			want:   ".upstream_flow_control_paused_reading_total",
		},
		{
			name:   "external service match upstream_flow_control_paused_reading_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_flow_control_paused_reading_total"},
			want:   ".upstream_flow_control_paused_reading_total",
		},
		{
			name:   "external service with subset match upstream_flow_control_paused_reading_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_flow_control_paused_reading_total"},
			want:   ".upstream_flow_control_paused_reading_total",
		},
		{
			name:   "kubernetes service match upstream_flow_control_resumed_reading_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_flow_control_resumed_reading_total"},
			want:   ".upstream_flow_control_resumed_reading_total",
		},
		{
			name:   "kubernetes service with subset match upstream_flow_control_resumed_reading_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_flow_control_resumed_reading_total"},
			want:   ".upstream_flow_control_resumed_reading_total",
		},
		{
			name:   "external service match upstream_flow_control_resumed_reading_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_flow_control_resumed_reading_total"},
			want:   ".upstream_flow_control_resumed_reading_total",
		},
		{
			name:   "external service with subset match upstream_flow_control_resumed_reading_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_flow_control_resumed_reading_total"},
			want:   ".upstream_flow_control_resumed_reading_total",
		},
		{
			name:   "kubernetes service match upstream_flow_control_backed_up_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_flow_control_backed_up_total"},
			want:   ".upstream_flow_control_backed_up_total",
		},
		{
			name:   "kubernetes service with subset match upstream_flow_control_backed_up_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_flow_control_backed_up_total"},
			want:   ".upstream_flow_control_backed_up_total",
		},
		{
			name:   "external service match upstream_flow_control_backed_up_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_flow_control_backed_up_total"},
			want:   ".upstream_flow_control_backed_up_total",
		},
		{
			name:   "external service with subset match upstream_flow_control_backed_up_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_flow_control_backed_up_total"},
			want:   ".upstream_flow_control_backed_up_total",
		},
		{
			name:   "kubernetes service match upstream_flow_control_drained_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_flow_control_drained_total"},
			want:   ".upstream_flow_control_drained_total",
		},
		{
			name:   "kubernetes service with subset match upstream_flow_control_drained_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_flow_control_drained_total"},
			want:   ".upstream_flow_control_drained_total",
		},
		{
			name:   "external service match upstream_flow_control_drained_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_flow_control_drained_total"},
			want:   ".upstream_flow_control_drained_total",
		},
		{
			name:   "external service with subset match upstream_flow_control_drained_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_flow_control_drained_total"},
			want:   ".upstream_flow_control_drained_total",
		},
		{
			name:   "kubernetes service match upstream_internal_redirect_failed_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_internal_redirect_failed_total"},
			want:   ".upstream_internal_redirect_failed_total",
		},
		{
			name:   "kubernetes service with subset match upstream_internal_redirect_failed_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_internal_redirect_failed_total"},
			want:   ".upstream_internal_redirect_failed_total",
		},
		{
			name:   "external service match upstream_internal_redirect_failed_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_internal_redirect_failed_total"},
			want:   ".upstream_internal_redirect_failed_total",
		},
		{
			name:   "external service with subset match upstream_internal_redirect_failed_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_internal_redirect_failed_total"},
			want:   ".upstream_internal_redirect_failed_total",
		},
		{
			name:   "kubernetes service match upstream_internal_redirect_succeeded_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_internal_redirect_succeeded_total"},
			want:   ".upstream_internal_redirect_succeeded_total",
		},
		{
			name:   "kubernetes service with subset match upstream_internal_redirect_succeeded_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_internal_redirect_succeeded_total"},
			want:   ".upstream_internal_redirect_succeeded_total",
		},
		{
			name:   "external service match upstream_internal_redirect_succeeded_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_internal_redirect_succeeded_total"},
			want:   ".upstream_internal_redirect_succeeded_total",
		},
		{
			name:   "external service with subset match upstream_internal_redirect_succeeded_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_internal_redirect_succeeded_total"},
			want:   ".upstream_internal_redirect_succeeded_total",
		},
		{
			name:   "kubernetes service match membership_change",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.membership_change"},
			want:   ".membership_change",
		},
		{
			name:   "kubernetes service with subset match membership_change",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.membership_change"},
			want:   ".membership_change",
		},
		{
			name:   "external service match membership_change",
			fields: fields{input: "cluster.outbound|443||istio.io.membership_change"},
			want:   ".membership_change",
		},
		{
			name:   "external service with subset match membership_change",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.membership_change"},
			want:   ".membership_change",
		},
		{
			name:   "kubernetes service match membership_healthy",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.membership_healthy"},
			want:   ".membership_healthy",
		},
		{
			name:   "kubernetes service with subset match membership_healthy",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.membership_healthy"},
			want:   ".membership_healthy",
		},
		{
			name:   "external service match membership_healthy",
			fields: fields{input: "cluster.outbound|443||istio.io.membership_healthy"},
			want:   ".membership_healthy",
		},
		{
			name:   "external service with subset match membership_healthy",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.membership_healthy"},
			want:   ".membership_healthy",
		},
		{
			name:   "kubernetes service match membership_degraded",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.membership_degraded"},
			want:   ".membership_degraded",
		},
		{
			name:   "kubernetes service with subset match membership_degraded",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.membership_degraded"},
			want:   ".membership_degraded",
		},
		{
			name:   "external service match membership_degraded",
			fields: fields{input: "cluster.outbound|443||istio.io.membership_degraded"},
			want:   ".membership_degraded",
		},
		{
			name:   "external service with subset match membership_degraded",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.membership_degraded"},
			want:   ".membership_degraded",
		},
		{
			name:   "kubernetes service match membership_excluded",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.membership_excluded"},
			want:   ".membership_excluded",
		},
		{
			name:   "kubernetes service with subset match membership_excluded",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.membership_excluded"},
			want:   ".membership_excluded",
		},
		{
			name:   "external service match membership_excluded",
			fields: fields{input: "cluster.outbound|443||istio.io.membership_excluded"},
			want:   ".membership_excluded",
		},
		{
			name:   "external service with subset match membership_excluded",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.membership_excluded"},
			want:   ".membership_excluded",
		},
		{
			name:   "kubernetes service match membership_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.membership_total"},
			want:   ".membership_total",
		},
		{
			name:   "kubernetes service with subset match membership_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.membership_total"},
			want:   ".membership_total",
		},
		{
			name:   "external service match membership_total",
			fields: fields{input: "cluster.outbound|443||istio.io.membership_total"},
			want:   ".membership_total",
		},
		{
			name:   "external service with subset match membership_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.membership_total"},
			want:   ".membership_total",
		},
		{
			name:   "kubernetes service match retry_or_shadow_abandoned",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.retry_or_shadow_abandoned"},
			want:   ".retry_or_shadow_abandoned",
		},
		{
			name:   "kubernetes service with subset match retry_or_shadow_abandoned",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.retry_or_shadow_abandoned"},
			want:   ".retry_or_shadow_abandoned",
		},
		{
			name:   "external service match retry_or_shadow_abandoned",
			fields: fields{input: "cluster.outbound|443||istio.io.retry_or_shadow_abandoned"},
			want:   ".retry_or_shadow_abandoned",
		},
		{
			name:   "external service with subset match retry_or_shadow_abandoned",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.retry_or_shadow_abandoned"},
			want:   ".retry_or_shadow_abandoned",
		},
		{
			name:   "kubernetes service match config_reload",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.config_reload"},
			want:   ".config_reload",
		},
		{
			name:   "kubernetes service with subset match config_reload",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.config_reload"},
			want:   ".config_reload",
		},
		{
			name:   "external service match config_reload",
			fields: fields{input: "cluster.outbound|443||istio.io.config_reload"},
			want:   ".config_reload",
		},
		{
			name:   "external service with subset match config_reload",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.config_reload"},
			want:   ".config_reload",
		},
		{
			name:   "kubernetes service match update_attempt",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.update_attempt"},
			want:   ".update_attempt",
		},
		{
			name:   "kubernetes service with subset match update_attempt",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.update_attempt"},
			want:   ".update_attempt",
		},
		{
			name:   "external service match update_attempt",
			fields: fields{input: "cluster.outbound|443||istio.io.update_attempt"},
			want:   ".update_attempt",
		},
		{
			name:   "external service with subset match update_attempt",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.update_attempt"},
			want:   ".update_attempt",
		},
		{
			name:   "kubernetes service match update_success",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.update_success"},
			want:   ".update_success",
		},
		{
			name:   "kubernetes service with subset match update_success",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.update_success"},
			want:   ".update_success",
		},
		{
			name:   "external service match update_success",
			fields: fields{input: "cluster.outbound|443||istio.io.update_success"},
			want:   ".update_success",
		},
		{
			name:   "external service with subset match update_success",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.update_success"},
			want:   ".update_success",
		},
		{
			name:   "kubernetes service match update_failure",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.update_failure"},
			want:   ".update_failure",
		},
		{
			name:   "kubernetes service with subset match update_failure",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.update_failure"},
			want:   ".update_failure",
		},
		{
			name:   "external service match update_failure",
			fields: fields{input: "cluster.outbound|443||istio.io.update_failure"},
			want:   ".update_failure",
		},
		{
			name:   "external service with subset match update_failure",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.update_failure"},
			want:   ".update_failure",
		},
		{
			name:   "kubernetes service match update_duration",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.update_duration"},
			want:   ".update_duration",
		},
		{
			name:   "kubernetes service with subset match update_duration",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.update_duration"},
			want:   ".update_duration",
		},
		{
			name:   "external service match update_duration",
			fields: fields{input: "cluster.outbound|443||istio.io.update_duration"},
			want:   ".update_duration",
		},
		{
			name:   "external service with subset match update_duration",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.update_duration"},
			want:   ".update_duration",
		},
		{
			name:   "kubernetes service match update_empty",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.update_empty"},
			want:   ".update_empty",
		},
		{
			name:   "kubernetes service with subset match update_empty",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.update_empty"},
			want:   ".update_empty",
		},
		{
			name:   "external service match update_empty",
			fields: fields{input: "cluster.outbound|443||istio.io.update_empty"},
			want:   ".update_empty",
		},
		{
			name:   "external service with subset match update_empty",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.update_empty"},
			want:   ".update_empty",
		},
		{
			name:   "kubernetes service match update_no_rebuild",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.update_no_rebuild"},
			want:   ".update_no_rebuild",
		},
		{
			name:   "kubernetes service with subset match update_no_rebuild",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.update_no_rebuild"},
			want:   ".update_no_rebuild",
		},
		{
			name:   "external service match update_no_rebuild",
			fields: fields{input: "cluster.outbound|443||istio.io.update_no_rebuild"},
			want:   ".update_no_rebuild",
		},
		{
			name:   "external service with subset match update_no_rebuild",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.update_no_rebuild"},
			want:   ".update_no_rebuild",
		},
		{
			name:   "kubernetes service match version",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.version"},
			want:   ".version",
		},
		{
			name:   "kubernetes service with subset match version",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.version"},
			want:   ".version",
		},
		{
			name:   "external service match version",
			fields: fields{input: "cluster.outbound|443||istio.io.version"},
			want:   ".version",
		},
		{
			name:   "external service with subset match version",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.version"},
			want:   ".version",
		},
		{
			name:   "kubernetes service match max_host_weight",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.max_host_weight"},
			want:   ".max_host_weight",
		},
		{
			name:   "kubernetes service with subset match max_host_weight",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.max_host_weight"},
			want:   ".max_host_weight",
		},
		{
			name:   "external service match max_host_weight",
			fields: fields{input: "cluster.outbound|443||istio.io.max_host_weight"},
			want:   ".max_host_weight",
		},
		{
			name:   "external service with subset match max_host_weight",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.max_host_weight"},
			want:   ".max_host_weight",
		},
		{
			name:   "kubernetes service match bind_errors",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.bind_errors"},
			want:   ".bind_errors",
		},
		{
			name:   "kubernetes service with subset match bind_errors",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.bind_errors"},
			want:   ".bind_errors",
		},
		{
			name:   "external service match bind_errors",
			fields: fields{input: "cluster.outbound|443||istio.io.bind_errors"},
			want:   ".bind_errors",
		},
		{
			name:   "external service with subset match bind_errors",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.bind_errors"},
			want:   ".bind_errors",
		},
		{
			name:   "kubernetes service match assignment_timeout_received",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.assignment_timeout_received"},
			want:   ".assignment_timeout_received",
		},
		{
			name:   "kubernetes service with subset match assignment_timeout_received",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.assignment_timeout_received"},
			want:   ".assignment_timeout_received",
		},
		{
			name:   "external service match assignment_timeout_received",
			fields: fields{input: "cluster.outbound|443||istio.io.assignment_timeout_received"},
			want:   ".assignment_timeout_received",
		},
		{
			name:   "external service with subset match assignment_timeout_received",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.assignment_timeout_received"},
			want:   ".assignment_timeout_received",
		},
		{
			name:   "kubernetes service match assignment_stale",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.assignment_stale"},
			want:   ".assignment_stale",
		},
		{
			name:   "kubernetes service with subset match assignment_stale",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.assignment_stale"},
			want:   ".assignment_stale",
		},
		{
			name:   "external service match assignment_stale",
			fields: fields{input: "cluster.outbound|443||istio.io.assignment_stale"},
			want:   ".assignment_stale",
		},
		{
			name:   "external service with subset match assignment_stale",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.assignment_stale"},
			want:   ".assignment_stale",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_200"},
			want:   ".upstream.tx.quic_connection_close_error_code_200",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_200"},
			want:   ".upstream.tx.quic_connection_close_error_code_200",
		},
		{
			name:   "external service match upstream.tx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_connection_close_error_code_200"},
			want:   ".upstream.tx.quic_connection_close_error_code_200",
		},
		{
			name:   "external service with subset match upstream.tx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_connection_close_error_code_200"},
			want:   ".upstream.tx.quic_connection_close_error_code_200",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_300"},
			want:   ".upstream.tx.quic_connection_close_error_code_300",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_300"},
			want:   ".upstream.tx.quic_connection_close_error_code_300",
		},
		{
			name:   "external service match upstream.tx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_connection_close_error_code_300"},
			want:   ".upstream.tx.quic_connection_close_error_code_300",
		},
		{
			name:   "external service with subset match upstream.tx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_connection_close_error_code_300"},
			want:   ".upstream.tx.quic_connection_close_error_code_300",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_400"},
			want:   ".upstream.tx.quic_connection_close_error_code_400",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_400"},
			want:   ".upstream.tx.quic_connection_close_error_code_400",
		},
		{
			name:   "external service match upstream.tx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_connection_close_error_code_400"},
			want:   ".upstream.tx.quic_connection_close_error_code_400",
		},
		{
			name:   "external service with subset match upstream.tx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_connection_close_error_code_400"},
			want:   ".upstream.tx.quic_connection_close_error_code_400",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_500"},
			want:   ".upstream.tx.quic_connection_close_error_code_500",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_connection_close_error_code_500"},
			want:   ".upstream.tx.quic_connection_close_error_code_500",
		},
		{
			name:   "external service match upstream.tx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_connection_close_error_code_500"},
			want:   ".upstream.tx.quic_connection_close_error_code_500",
		},
		{
			name:   "external service with subset match upstream.tx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_connection_close_error_code_500"},
			want:   ".upstream.tx.quic_connection_close_error_code_500",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_200"},
			want:   ".upstream.rx.quic_connection_close_error_code_200",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_200"},
			want:   ".upstream.rx.quic_connection_close_error_code_200",
		},
		{
			name:   "external service match upstream.rx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_connection_close_error_code_200"},
			want:   ".upstream.rx.quic_connection_close_error_code_200",
		},
		{
			name:   "external service with subset match upstream.rx.quic_connection_close_error_code_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_connection_close_error_code_200"},
			want:   ".upstream.rx.quic_connection_close_error_code_200",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_300"},
			want:   ".upstream.rx.quic_connection_close_error_code_300",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_300"},
			want:   ".upstream.rx.quic_connection_close_error_code_300",
		},
		{
			name:   "external service match upstream.rx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_connection_close_error_code_300"},
			want:   ".upstream.rx.quic_connection_close_error_code_300",
		},
		{
			name:   "external service with subset match upstream.rx.quic_connection_close_error_code_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_connection_close_error_code_300"},
			want:   ".upstream.rx.quic_connection_close_error_code_300",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_400"},
			want:   ".upstream.rx.quic_connection_close_error_code_400",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_400"},
			want:   ".upstream.rx.quic_connection_close_error_code_400",
		},
		{
			name:   "external service match upstream.rx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_connection_close_error_code_400"},
			want:   ".upstream.rx.quic_connection_close_error_code_400",
		},
		{
			name:   "external service with subset match upstream.rx.quic_connection_close_error_code_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_connection_close_error_code_400"},
			want:   ".upstream.rx.quic_connection_close_error_code_400",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_500"},
			want:   ".upstream.rx.quic_connection_close_error_code_500",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_connection_close_error_code_500"},
			want:   ".upstream.rx.quic_connection_close_error_code_500",
		},
		{
			name:   "external service match upstream.rx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_connection_close_error_code_500"},
			want:   ".upstream.rx.quic_connection_close_error_code_500",
		},
		{
			name:   "external service with subset match upstream.rx.quic_connection_close_error_code_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_connection_close_error_code_500"},
			want:   ".upstream.rx.quic_connection_close_error_code_500",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_200"},
			want:   ".upstream.tx.quic_reset_stream_error_code_200",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_200"},
			want:   ".upstream.tx.quic_reset_stream_error_code_200",
		},
		{
			name:   "external service match upstream.tx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_reset_stream_error_code_200"},
			want:   ".upstream.tx.quic_reset_stream_error_code_200",
		},
		{
			name:   "external service with subset match upstream.tx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_reset_stream_error_code_200"},
			want:   ".upstream.tx.quic_reset_stream_error_code_200",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_300"},
			want:   ".upstream.tx.quic_reset_stream_error_code_300",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_300"},
			want:   ".upstream.tx.quic_reset_stream_error_code_300",
		},
		{
			name:   "external service match upstream.tx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_reset_stream_error_code_300"},
			want:   ".upstream.tx.quic_reset_stream_error_code_300",
		},
		{
			name:   "external service with subset match upstream.tx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_reset_stream_error_code_300"},
			want:   ".upstream.tx.quic_reset_stream_error_code_300",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_400"},
			want:   ".upstream.tx.quic_reset_stream_error_code_400",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_400"},
			want:   ".upstream.tx.quic_reset_stream_error_code_400",
		},
		{
			name:   "external service match upstream.tx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_reset_stream_error_code_400"},
			want:   ".upstream.tx.quic_reset_stream_error_code_400",
		},
		{
			name:   "external service with subset match upstream.tx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_reset_stream_error_code_400"},
			want:   ".upstream.tx.quic_reset_stream_error_code_400",
		},
		{
			name:   "kubernetes service match upstream.tx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_500"},
			want:   ".upstream.tx.quic_reset_stream_error_code_500",
		},
		{
			name:   "kubernetes service with subset match upstream.tx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.tx.quic_reset_stream_error_code_500"},
			want:   ".upstream.tx.quic_reset_stream_error_code_500",
		},
		{
			name:   "external service match upstream.tx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.tx.quic_reset_stream_error_code_500"},
			want:   ".upstream.tx.quic_reset_stream_error_code_500",
		},
		{
			name:   "external service with subset match upstream.tx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.tx.quic_reset_stream_error_code_500"},
			want:   ".upstream.tx.quic_reset_stream_error_code_500",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_200"},
			want:   ".upstream.rx.quic_reset_stream_error_code_200",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_200"},
			want:   ".upstream.rx.quic_reset_stream_error_code_200",
		},
		{
			name:   "external service match upstream.rx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_reset_stream_error_code_200"},
			want:   ".upstream.rx.quic_reset_stream_error_code_200",
		},
		{
			name:   "external service with subset match upstream.rx.quic_reset_stream_error_code_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_reset_stream_error_code_200"},
			want:   ".upstream.rx.quic_reset_stream_error_code_200",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_300"},
			want:   ".upstream.rx.quic_reset_stream_error_code_300",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_300"},
			want:   ".upstream.rx.quic_reset_stream_error_code_300",
		},
		{
			name:   "external service match upstream.rx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_reset_stream_error_code_300"},
			want:   ".upstream.rx.quic_reset_stream_error_code_300",
		},
		{
			name:   "external service with subset match upstream.rx.quic_reset_stream_error_code_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_reset_stream_error_code_300"},
			want:   ".upstream.rx.quic_reset_stream_error_code_300",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_400"},
			want:   ".upstream.rx.quic_reset_stream_error_code_400",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_400"},
			want:   ".upstream.rx.quic_reset_stream_error_code_400",
		},
		{
			name:   "external service match upstream.rx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_reset_stream_error_code_400"},
			want:   ".upstream.rx.quic_reset_stream_error_code_400",
		},
		{
			name:   "external service with subset match upstream.rx.quic_reset_stream_error_code_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_reset_stream_error_code_400"},
			want:   ".upstream.rx.quic_reset_stream_error_code_400",
		},
		{
			name:   "kubernetes service match upstream.rx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_500"},
			want:   ".upstream.rx.quic_reset_stream_error_code_500",
		},
		{
			name:   "kubernetes service with subset match upstream.rx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream.rx.quic_reset_stream_error_code_500"},
			want:   ".upstream.rx.quic_reset_stream_error_code_500",
		},
		{
			name:   "external service match upstream.rx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream.rx.quic_reset_stream_error_code_500"},
			want:   ".upstream.rx.quic_reset_stream_error_code_500",
		},
		{
			name:   "external service with subset match upstream.rx.quic_reset_stream_error_code_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream.rx.quic_reset_stream_error_code_500"},
			want:   ".upstream.rx.quic_reset_stream_error_code_500",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_total"},
			want:   ".outlier_detection.ejections_enforced_total",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_total"},
			want:   ".outlier_detection.ejections_enforced_total",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_total",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_total"},
			want:   ".outlier_detection.ejections_enforced_total",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_total"},
			want:   ".outlier_detection.ejections_enforced_total",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_active"},
			want:   ".outlier_detection.ejections_active",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_active"},
			want:   ".outlier_detection.ejections_active",
		},
		{
			name:   "external service match outlier_detection.ejections_active",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_active"},
			want:   ".outlier_detection.ejections_active",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_active",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_active"},
			want:   ".outlier_detection.ejections_active",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_overflow",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_overflow"},
			want:   ".outlier_detection.ejections_overflow",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_overflow",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_overflow"},
			want:   ".outlier_detection.ejections_overflow",
		},
		{
			name:   "external service match outlier_detection.ejections_overflow",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_overflow"},
			want:   ".outlier_detection.ejections_overflow",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_overflow",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_overflow"},
			want:   ".outlier_detection.ejections_overflow",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_consecutive_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_consecutive_5xx"},
			want:   ".outlier_detection.ejections_enforced_consecutive_5xx",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_consecutive_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_consecutive_5xx"},
			want:   ".outlier_detection.ejections_enforced_consecutive_5xx",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_consecutive_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_consecutive_5xx"},
			want:   ".outlier_detection.ejections_enforced_consecutive_5xx",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_consecutive_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_consecutive_5xx"},
			want:   ".outlier_detection.ejections_enforced_consecutive_5xx",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_consecutive_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_consecutive_5xx"},
			want:   ".outlier_detection.ejections_detected_consecutive_5xx",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_consecutive_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_consecutive_5xx"},
			want:   ".outlier_detection.ejections_detected_consecutive_5xx",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_consecutive_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_consecutive_5xx"},
			want:   ".outlier_detection.ejections_detected_consecutive_5xx",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_consecutive_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_consecutive_5xx"},
			want:   ".outlier_detection.ejections_detected_consecutive_5xx",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_success_rate",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_success_rate"},
			want:   ".outlier_detection.ejections_enforced_success_rate",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_success_rate",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_success_rate"},
			want:   ".outlier_detection.ejections_enforced_success_rate",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_success_rate",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_success_rate"},
			want:   ".outlier_detection.ejections_enforced_success_rate",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_success_rate",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_success_rate"},
			want:   ".outlier_detection.ejections_enforced_success_rate",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_success_rate",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_success_rate"},
			want:   ".outlier_detection.ejections_detected_success_rate",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_success_rate",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_success_rate"},
			want:   ".outlier_detection.ejections_detected_success_rate",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_success_rate",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_success_rate"},
			want:   ".outlier_detection.ejections_detected_success_rate",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_success_rate",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_success_rate"},
			want:   ".outlier_detection.ejections_detected_success_rate",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_gateway_failure",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_gateway_failure",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_gateway_failure",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_gateway_failure",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_gateway_failure",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_gateway_failure",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_gateway_failure",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_consecutive_gateway_failure",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_consecutive_gateway_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_gateway_failure",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_local_origin_failure",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_local_origin_failure",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_local_origin_failure",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_enforced_consecutive_local_origin_failure",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_local_origin_failure",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_local_origin_failure",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_local_origin_failure",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_consecutive_local_origin_failure",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_consecutive_local_origin_failure"},
			want:   ".outlier_detection.ejections_detected_consecutive_local_origin_failure",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_enforced_local_origin_success_rate",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_enforced_local_origin_success_rate",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_enforced_local_origin_success_rate",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_enforced_local_origin_success_rate",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_detected_local_origin_success_rate",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_detected_local_origin_success_rate",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_detected_local_origin_success_rate",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_local_origin_success_rate",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_local_origin_success_rate"},
			want:   ".outlier_detection.ejections_detected_local_origin_success_rate",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_failure_percentage",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_failure_percentage"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_failure_percentage",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_failure_percentage"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_failure_percentage",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_failure_percentage"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_failure_percentage",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_failure_percentage"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_failure_percentage",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_failure_percentage"},
			want:   ".outlier_detection.ejections_detected_failure_percentage",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_failure_percentage",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_failure_percentage"},
			want:   ".outlier_detection.ejections_detected_failure_percentage",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_failure_percentage",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_failure_percentage"},
			want:   ".outlier_detection.ejections_detected_failure_percentage",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_failure_percentage",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_failure_percentage"},
			want:   ".outlier_detection.ejections_detected_failure_percentage",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_enforced_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage_local_origin",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_enforced_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_enforced_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage_local_origin",
		},
		{
			name:   "external service match outlier_detection.ejections_enforced_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_enforced_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage_local_origin",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_enforced_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_enforced_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_enforced_failure_percentage_local_origin",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_detected_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_detected_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_detected_failure_percentage_local_origin",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_detected_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_detected_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_detected_failure_percentage_local_origin",
		},
		{
			name:   "external service match outlier_detection.ejections_detected_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_detected_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_detected_failure_percentage_local_origin",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_detected_failure_percentage_local_origin",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_detected_failure_percentage_local_origin"},
			want:   ".outlier_detection.ejections_detected_failure_percentage_local_origin",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_total"},
			want:   ".outlier_detection.ejections_total",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_total"},
			want:   ".outlier_detection.ejections_total",
		},
		{
			name:   "external service match outlier_detection.ejections_total",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_total"},
			want:   ".outlier_detection.ejections_total",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_total",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_total"},
			want:   ".outlier_detection.ejections_total",
		},
		{
			name:   "kubernetes service match outlier_detection.ejections_consecutive_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.outlier_detection.ejections_consecutive_5xx"},
			want:   ".outlier_detection.ejections_consecutive_5xx",
		},
		{
			name:   "kubernetes service with subset match outlier_detection.ejections_consecutive_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.outlier_detection.ejections_consecutive_5xx"},
			want:   ".outlier_detection.ejections_consecutive_5xx",
		},
		{
			name:   "external service match outlier_detection.ejections_consecutive_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.outlier_detection.ejections_consecutive_5xx"},
			want:   ".outlier_detection.ejections_consecutive_5xx",
		},
		{
			name:   "external service with subset match outlier_detection.ejections_consecutive_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.outlier_detection.ejections_consecutive_5xx"},
			want:   ".outlier_detection.ejections_consecutive_5xx",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.cx_open",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.cx_open"},
			want:   ".circuit_breakers.HIGH.cx_open",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.cx_open",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.cx_open"},
			want:   ".circuit_breakers.HIGH.cx_open",
		},
		{
			name:   "external service match circuit_breakers.HIGH.cx_open",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.cx_open"},
			want:   ".circuit_breakers.HIGH.cx_open",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.cx_open",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.cx_open"},
			want:   ".circuit_breakers.HIGH.cx_open",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.cx_pool_open",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.cx_pool_open"},
			want:   ".circuit_breakers.HIGH.cx_pool_open",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.cx_pool_open",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.cx_pool_open"},
			want:   ".circuit_breakers.HIGH.cx_pool_open",
		},
		{
			name:   "external service match circuit_breakers.HIGH.cx_pool_open",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.cx_pool_open"},
			want:   ".circuit_breakers.HIGH.cx_pool_open",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.cx_pool_open",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.cx_pool_open"},
			want:   ".circuit_breakers.HIGH.cx_pool_open",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.rq_pending_open",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.rq_pending_open"},
			want:   ".circuit_breakers.HIGH.rq_pending_open",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.rq_pending_open",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.rq_pending_open"},
			want:   ".circuit_breakers.HIGH.rq_pending_open",
		},
		{
			name:   "external service match circuit_breakers.HIGH.rq_pending_open",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.rq_pending_open"},
			want:   ".circuit_breakers.HIGH.rq_pending_open",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.rq_pending_open",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.rq_pending_open"},
			want:   ".circuit_breakers.HIGH.rq_pending_open",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.rq_open",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.rq_open"},
			want:   ".circuit_breakers.HIGH.rq_open",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.rq_open",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.rq_open"},
			want:   ".circuit_breakers.HIGH.rq_open",
		},
		{
			name:   "external service match circuit_breakers.HIGH.rq_open",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.rq_open"},
			want:   ".circuit_breakers.HIGH.rq_open",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.rq_open",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.rq_open"},
			want:   ".circuit_breakers.HIGH.rq_open",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.rq_retry_open",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.rq_retry_open"},
			want:   ".circuit_breakers.HIGH.rq_retry_open",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.rq_retry_open",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.rq_retry_open"},
			want:   ".circuit_breakers.HIGH.rq_retry_open",
		},
		{
			name:   "external service match circuit_breakers.HIGH.rq_retry_open",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.rq_retry_open"},
			want:   ".circuit_breakers.HIGH.rq_retry_open",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.rq_retry_open",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.rq_retry_open"},
			want:   ".circuit_breakers.HIGH.rq_retry_open",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.remaining_cx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_cx"},
			want:   ".circuit_breakers.HIGH.remaining_cx",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.remaining_cx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_cx"},
			want:   ".circuit_breakers.HIGH.remaining_cx",
		},
		{
			name:   "external service match circuit_breakers.HIGH.remaining_cx",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.remaining_cx"},
			want:   ".circuit_breakers.HIGH.remaining_cx",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.remaining_cx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.remaining_cx"},
			want:   ".circuit_breakers.HIGH.remaining_cx",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.remaining_pending",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_pending"},
			want:   ".circuit_breakers.HIGH.remaining_pending",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.remaining_pending",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_pending"},
			want:   ".circuit_breakers.HIGH.remaining_pending",
		},
		{
			name:   "external service match circuit_breakers.HIGH.remaining_pending",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.remaining_pending"},
			want:   ".circuit_breakers.HIGH.remaining_pending",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.remaining_pending",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.remaining_pending"},
			want:   ".circuit_breakers.HIGH.remaining_pending",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.remaining_rq",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_rq"},
			want:   ".circuit_breakers.HIGH.remaining_rq",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.remaining_rq",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_rq"},
			want:   ".circuit_breakers.HIGH.remaining_rq",
		},
		{
			name:   "external service match circuit_breakers.HIGH.remaining_rq",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.remaining_rq"},
			want:   ".circuit_breakers.HIGH.remaining_rq",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.remaining_rq",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.remaining_rq"},
			want:   ".circuit_breakers.HIGH.remaining_rq",
		},
		{
			name:   "kubernetes service match circuit_breakers.HIGH.remaining_retries",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_retries"},
			want:   ".circuit_breakers.HIGH.remaining_retries",
		},
		{
			name:   "kubernetes service with subset match circuit_breakers.HIGH.remaining_retries",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.circuit_breakers.HIGH.remaining_retries"},
			want:   ".circuit_breakers.HIGH.remaining_retries",
		},
		{
			name:   "external service match circuit_breakers.HIGH.remaining_retries",
			fields: fields{input: "cluster.outbound|443||istio.io.circuit_breakers.HIGH.remaining_retries"},
			want:   ".circuit_breakers.HIGH.remaining_retries",
		},
		{
			name:   "external service with subset match circuit_breakers.HIGH.remaining_retries",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.circuit_breakers.HIGH.remaining_retries"},
			want:   ".circuit_breakers.HIGH.remaining_retries",
		},
		{
			name:   "kubernetes service match upstream_rq_timeout_budget_percent_used",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_timeout_budget_percent_used"},
			want:   ".upstream_rq_timeout_budget_percent_used",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_timeout_budget_percent_used",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_timeout_budget_percent_used"},
			want:   ".upstream_rq_timeout_budget_percent_used",
		},
		{
			name:   "external service match upstream_rq_timeout_budget_percent_used",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_timeout_budget_percent_used"},
			want:   ".upstream_rq_timeout_budget_percent_used",
		},
		{
			name:   "external service with subset match upstream_rq_timeout_budget_percent_used",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_timeout_budget_percent_used"},
			want:   ".upstream_rq_timeout_budget_percent_used",
		},
		{
			name:   "kubernetes service match upstream_rq_timeout_budget_per_try_percent_used",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_timeout_budget_per_try_percent_used"},
			want:   ".upstream_rq_timeout_budget_per_try_percent_used",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_timeout_budget_per_try_percent_used",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_timeout_budget_per_try_percent_used"},
			want:   ".upstream_rq_timeout_budget_per_try_percent_used",
		},
		{
			name:   "external service match upstream_rq_timeout_budget_per_try_percent_used",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_timeout_budget_per_try_percent_used"},
			want:   ".upstream_rq_timeout_budget_per_try_percent_used",
		},
		{
			name:   "external service with subset match upstream_rq_timeout_budget_per_try_percent_used",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_timeout_budget_per_try_percent_used"},
			want:   ".upstream_rq_timeout_budget_per_try_percent_used",
		},
		{
			name:   "kubernetes service match upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_completed"},
			want:   ".upstream_rq_completed",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_completed"},
			want:   ".upstream_rq_completed",
		},
		{
			name:   "external service match upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_completed"},
			want:   ".upstream_rq_completed",
		},
		{
			name:   "external service with subset match upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_completed"},
			want:   ".upstream_rq_completed",
		},
		{
			name:   "kubernetes service match upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_2xx"},
			want:   ".upstream_rq_2xx",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_2xx"},
			want:   ".upstream_rq_2xx",
		},
		{
			name:   "external service match upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_2xx"},
			want:   ".upstream_rq_2xx",
		},
		{
			name:   "external service with subset match upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_2xx"},
			want:   ".upstream_rq_2xx",
		},
		{
			name:   "kubernetes service match upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_3xx"},
			want:   ".upstream_rq_3xx",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_3xx"},
			want:   ".upstream_rq_3xx",
		},
		{
			name:   "external service match upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_3xx"},
			want:   ".upstream_rq_3xx",
		},
		{
			name:   "external service with subset match upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_3xx"},
			want:   ".upstream_rq_3xx",
		},
		{
			name:   "kubernetes service match upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_4xx"},
			want:   ".upstream_rq_4xx",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_4xx"},
			want:   ".upstream_rq_4xx",
		},
		{
			name:   "external service match upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_4xx"},
			want:   ".upstream_rq_4xx",
		},
		{
			name:   "external service with subset match upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_4xx"},
			want:   ".upstream_rq_4xx",
		},
		{
			name:   "kubernetes service match upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_5xx"},
			want:   ".upstream_rq_5xx",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_5xx"},
			want:   ".upstream_rq_5xx",
		},
		{
			name:   "external service match upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_5xx"},
			want:   ".upstream_rq_5xx",
		},
		{
			name:   "external service with subset match upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_5xx"},
			want:   ".upstream_rq_5xx",
		},
		{
			name:   "kubernetes service match upstream_rq_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_200"},
			want:   ".upstream_rq_200",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_200"},
			want:   ".upstream_rq_200",
		},
		{
			name:   "external service match upstream_rq_200",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_200"},
			want:   ".upstream_rq_200",
		},
		{
			name:   "external service with subset match upstream_rq_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_200"},
			want:   ".upstream_rq_200",
		},
		{
			name:   "kubernetes service match upstream_rq_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_300"},
			want:   ".upstream_rq_300",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_300"},
			want:   ".upstream_rq_300",
		},
		{
			name:   "external service match upstream_rq_300",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_300"},
			want:   ".upstream_rq_300",
		},
		{
			name:   "external service with subset match upstream_rq_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_300"},
			want:   ".upstream_rq_300",
		},
		{
			name:   "kubernetes service match upstream_rq_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_400"},
			want:   ".upstream_rq_400",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_400"},
			want:   ".upstream_rq_400",
		},
		{
			name:   "external service match upstream_rq_400",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_400"},
			want:   ".upstream_rq_400",
		},
		{
			name:   "external service with subset match upstream_rq_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_400"},
			want:   ".upstream_rq_400",
		},
		{
			name:   "kubernetes service match upstream_rq_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_500"},
			want:   ".upstream_rq_500",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_500"},
			want:   ".upstream_rq_500",
		},
		{
			name:   "external service match upstream_rq_500",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_500"},
			want:   ".upstream_rq_500",
		},
		{
			name:   "external service with subset match upstream_rq_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_500"},
			want:   ".upstream_rq_500",
		},
		{
			name:   "kubernetes service match upstream_rq_time",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_time"},
			want:   ".upstream_rq_time",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_time",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_time"},
			want:   ".upstream_rq_time",
		},
		{
			name:   "external service match upstream_rq_time",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_time"},
			want:   ".upstream_rq_time",
		},
		{
			name:   "external service with subset match upstream_rq_time",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_time"},
			want:   ".upstream_rq_time",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_completed"},
			want:   ".canary.upstream_rq_completed",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_completed"},
			want:   ".canary.upstream_rq_completed",
		},
		{
			name:   "external service match canary.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_completed"},
			want:   ".canary.upstream_rq_completed",
		},
		{
			name:   "external service with subset match canary.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_completed"},
			want:   ".canary.upstream_rq_completed",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_2xx"},
			want:   ".canary.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_2xx"},
			want:   ".canary.upstream_rq_2xx",
		},
		{
			name:   "external service match canary.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_2xx"},
			want:   ".canary.upstream_rq_2xx",
		},
		{
			name:   "external service with subset match canary.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_2xx"},
			want:   ".canary.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_3xx"},
			want:   ".canary.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_3xx"},
			want:   ".canary.upstream_rq_3xx",
		},
		{
			name:   "external service match canary.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_3xx"},
			want:   ".canary.upstream_rq_3xx",
		},
		{
			name:   "external service with subset match canary.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_3xx"},
			want:   ".canary.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_4xx"},
			want:   ".canary.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_4xx"},
			want:   ".canary.upstream_rq_4xx",
		},
		{
			name:   "external service match canary.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_4xx"},
			want:   ".canary.upstream_rq_4xx",
		},
		{
			name:   "external service with subset match canary.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_4xx"},
			want:   ".canary.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_5xx"},
			want:   ".canary.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_5xx"},
			want:   ".canary.upstream_rq_5xx",
		},
		{
			name:   "external service match canary.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_5xx"},
			want:   ".canary.upstream_rq_5xx",
		},
		{
			name:   "external service with subset match canary.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_5xx"},
			want:   ".canary.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_200"},
			want:   ".canary.upstream_rq_200",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_200"},
			want:   ".canary.upstream_rq_200",
		},
		{
			name:   "external service match canary.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_200"},
			want:   ".canary.upstream_rq_200",
		},
		{
			name:   "external service with subset match canary.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_200"},
			want:   ".canary.upstream_rq_200",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_300"},
			want:   ".canary.upstream_rq_300",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_300"},
			want:   ".canary.upstream_rq_300",
		},
		{
			name:   "external service match canary.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_300"},
			want:   ".canary.upstream_rq_300",
		},
		{
			name:   "external service with subset match canary.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_300"},
			want:   ".canary.upstream_rq_300",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_400"},
			want:   ".canary.upstream_rq_400",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_400"},
			want:   ".canary.upstream_rq_400",
		},
		{
			name:   "external service match canary.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_400"},
			want:   ".canary.upstream_rq_400",
		},
		{
			name:   "external service with subset match canary.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_400"},
			want:   ".canary.upstream_rq_400",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_500"},
			want:   ".canary.upstream_rq_500",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_500"},
			want:   ".canary.upstream_rq_500",
		},
		{
			name:   "external service match canary.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_500"},
			want:   ".canary.upstream_rq_500",
		},
		{
			name:   "external service with subset match canary.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_500"},
			want:   ".canary.upstream_rq_500",
		},
		{
			name:   "kubernetes service match canary.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.canary.upstream_rq_time"},
			want:   ".canary.upstream_rq_time",
		},
		{
			name:   "kubernetes service with subset match canary.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.canary.upstream_rq_time"},
			want:   ".canary.upstream_rq_time",
		},
		{
			name:   "external service match canary.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443||istio.io.canary.upstream_rq_time"},
			want:   ".canary.upstream_rq_time",
		},
		{
			name:   "external service with subset match canary.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.canary.upstream_rq_time"},
			want:   ".canary.upstream_rq_time",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_completed"},
			want:   ".internal.upstream_rq_completed",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_completed"},
			want:   ".internal.upstream_rq_completed",
		},
		{
			name:   "external service match internal.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_completed"},
			want:   ".internal.upstream_rq_completed",
		},
		{
			name:   "external service with subset match internal.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_completed"},
			want:   ".internal.upstream_rq_completed",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_2xx"},
			want:   ".internal.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_2xx"},
			want:   ".internal.upstream_rq_2xx",
		},
		{
			name:   "external service match internal.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_2xx"},
			want:   ".internal.upstream_rq_2xx",
		},
		{
			name:   "external service with subset match internal.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_2xx"},
			want:   ".internal.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_3xx"},
			want:   ".internal.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_3xx"},
			want:   ".internal.upstream_rq_3xx",
		},
		{
			name:   "external service match internal.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_3xx"},
			want:   ".internal.upstream_rq_3xx",
		},
		{
			name:   "external service with subset match internal.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_3xx"},
			want:   ".internal.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_4xx"},
			want:   ".internal.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_4xx"},
			want:   ".internal.upstream_rq_4xx",
		},
		{
			name:   "external service match internal.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_4xx"},
			want:   ".internal.upstream_rq_4xx",
		},
		{
			name:   "external service with subset match internal.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_4xx"},
			want:   ".internal.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_5xx"},
			want:   ".internal.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_5xx"},
			want:   ".internal.upstream_rq_5xx",
		},
		{
			name:   "external service match internal.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_5xx"},
			want:   ".internal.upstream_rq_5xx",
		},
		{
			name:   "external service with subset match internal.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_5xx"},
			want:   ".internal.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_200"},
			want:   ".internal.upstream_rq_200",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_200"},
			want:   ".internal.upstream_rq_200",
		},
		{
			name:   "external service match internal.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_200"},
			want:   ".internal.upstream_rq_200",
		},
		{
			name:   "external service with subset match internal.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_200"},
			want:   ".internal.upstream_rq_200",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_300"},
			want:   ".internal.upstream_rq_300",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_300"},
			want:   ".internal.upstream_rq_300",
		},
		{
			name:   "external service match internal.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_300"},
			want:   ".internal.upstream_rq_300",
		},
		{
			name:   "external service with subset match internal.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_300"},
			want:   ".internal.upstream_rq_300",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_400"},
			want:   ".internal.upstream_rq_400",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_400"},
			want:   ".internal.upstream_rq_400",
		},
		{
			name:   "external service match internal.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_400"},
			want:   ".internal.upstream_rq_400",
		},
		{
			name:   "external service with subset match internal.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_400"},
			want:   ".internal.upstream_rq_400",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_500"},
			want:   ".internal.upstream_rq_500",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_500"},
			want:   ".internal.upstream_rq_500",
		},
		{
			name:   "external service match internal.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_500"},
			want:   ".internal.upstream_rq_500",
		},
		{
			name:   "external service with subset match internal.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_500"},
			want:   ".internal.upstream_rq_500",
		},
		{
			name:   "kubernetes service match internal.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.internal.upstream_rq_time"},
			want:   ".internal.upstream_rq_time",
		},
		{
			name:   "kubernetes service with subset match internal.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.internal.upstream_rq_time"},
			want:   ".internal.upstream_rq_time",
		},
		{
			name:   "external service match internal.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443||istio.io.internal.upstream_rq_time"},
			want:   ".internal.upstream_rq_time",
		},
		{
			name:   "external service with subset match internal.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.internal.upstream_rq_time"},
			want:   ".internal.upstream_rq_time",
		},
		{
			name:   "kubernetes service match external.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_completed"},
			want:   ".external.upstream_rq_completed",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_completed"},
			want:   ".external.upstream_rq_completed",
		},
		{
			name:   "external service match external.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_completed"},
			want:   ".external.upstream_rq_completed",
		},
		{
			name:   "external service with subset match external.upstream_rq_completed",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_completed"},
			want:   ".external.upstream_rq_completed",
		},
		{
			name:   "kubernetes service match external.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_2xx"},
			want:   ".external.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_2xx"},
			want:   ".external.upstream_rq_2xx",
		},
		{
			name:   "external service match external.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_2xx"},
			want:   ".external.upstream_rq_2xx",
		},
		{
			name:   "external service with subset match external.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_2xx"},
			want:   ".external.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service match external.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_3xx"},
			want:   ".external.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_3xx"},
			want:   ".external.upstream_rq_3xx",
		},
		{
			name:   "external service match external.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_3xx"},
			want:   ".external.upstream_rq_3xx",
		},
		{
			name:   "external service with subset match external.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_3xx"},
			want:   ".external.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service match external.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_4xx"},
			want:   ".external.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_4xx"},
			want:   ".external.upstream_rq_4xx",
		},
		{
			name:   "external service match external.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_4xx"},
			want:   ".external.upstream_rq_4xx",
		},
		{
			name:   "external service with subset match external.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_4xx"},
			want:   ".external.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service match external.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_5xx"},
			want:   ".external.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_5xx"},
			want:   ".external.upstream_rq_5xx",
		},
		{
			name:   "external service match external.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_5xx"},
			want:   ".external.upstream_rq_5xx",
		},
		{
			name:   "external service with subset match external.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_5xx"},
			want:   ".external.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service match external.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_200"},
			want:   ".external.upstream_rq_200",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_200"},
			want:   ".external.upstream_rq_200",
		},
		{
			name:   "external service match external.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_200"},
			want:   ".external.upstream_rq_200",
		},
		{
			name:   "external service with subset match external.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_200"},
			want:   ".external.upstream_rq_200",
		},
		{
			name:   "kubernetes service match external.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_300"},
			want:   ".external.upstream_rq_300",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_300"},
			want:   ".external.upstream_rq_300",
		},
		{
			name:   "external service match external.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_300"},
			want:   ".external.upstream_rq_300",
		},
		{
			name:   "external service with subset match external.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_300"},
			want:   ".external.upstream_rq_300",
		},
		{
			name:   "kubernetes service match external.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_400"},
			want:   ".external.upstream_rq_400",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_400"},
			want:   ".external.upstream_rq_400",
		},
		{
			name:   "external service match external.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_400"},
			want:   ".external.upstream_rq_400",
		},
		{
			name:   "external service with subset match external.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_400"},
			want:   ".external.upstream_rq_400",
		},
		{
			name:   "kubernetes service match external.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_500"},
			want:   ".external.upstream_rq_500",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_500"},
			want:   ".external.upstream_rq_500",
		},
		{
			name:   "external service match external.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_500"},
			want:   ".external.upstream_rq_500",
		},
		{
			name:   "external service with subset match external.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_500"},
			want:   ".external.upstream_rq_500",
		},
		{
			name:   "kubernetes service match external.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.external.upstream_rq_time"},
			want:   ".external.upstream_rq_time",
		},
		{
			name:   "kubernetes service with subset match external.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.external.upstream_rq_time"},
			want:   ".external.upstream_rq_time",
		},
		{
			name:   "external service match external.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443||istio.io.external.upstream_rq_time"},
			want:   ".external.upstream_rq_time",
		},
		{
			name:   "external service with subset match external.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.external.upstream_rq_time"},
			want:   ".external.upstream_rq_time",
		},
		{
			name:   "kubernetes service match ssl.connection_error",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.connection_error"},
			want:   ".ssl.connection_error",
		},
		{
			name:   "kubernetes service with subset match ssl.connection_error",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.connection_error"},
			want:   ".ssl.connection_error",
		},
		{
			name:   "external service match ssl.connection_error",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.connection_error"},
			want:   ".ssl.connection_error",
		},
		{
			name:   "external service with subset match ssl.connection_error",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.connection_error"},
			want:   ".ssl.connection_error",
		},
		{
			name:   "kubernetes service match ssl.handshake",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.handshake"},
			want:   ".ssl.handshake",
		},
		{
			name:   "kubernetes service with subset match ssl.handshake",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.handshake"},
			want:   ".ssl.handshake",
		},
		{
			name:   "external service match ssl.handshake",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.handshake"},
			want:   ".ssl.handshake",
		},
		{
			name:   "external service with subset match ssl.handshake",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.handshake"},
			want:   ".ssl.handshake",
		},
		{
			name:   "kubernetes service match ssl.session_reused",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.session_reused"},
			want:   ".ssl.session_reused",
		},
		{
			name:   "kubernetes service with subset match ssl.session_reused",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.session_reused"},
			want:   ".ssl.session_reused",
		},
		{
			name:   "external service match ssl.session_reused",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.session_reused"},
			want:   ".ssl.session_reused",
		},
		{
			name:   "external service with subset match ssl.session_reused",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.session_reused"},
			want:   ".ssl.session_reused",
		},
		{
			name:   "kubernetes service match ssl.no_certificate",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.no_certificate"},
			want:   ".ssl.no_certificate",
		},
		{
			name:   "kubernetes service with subset match ssl.no_certificate",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.no_certificate"},
			want:   ".ssl.no_certificate",
		},
		{
			name:   "external service match ssl.no_certificate",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.no_certificate"},
			want:   ".ssl.no_certificate",
		},
		{
			name:   "external service with subset match ssl.no_certificate",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.no_certificate"},
			want:   ".ssl.no_certificate",
		},
		{
			name:   "kubernetes service match ssl.fail_verify_no_cert",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.fail_verify_no_cert"},
			want:   ".ssl.fail_verify_no_cert",
		},
		{
			name:   "kubernetes service with subset match ssl.fail_verify_no_cert",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.fail_verify_no_cert"},
			want:   ".ssl.fail_verify_no_cert",
		},
		{
			name:   "external service match ssl.fail_verify_no_cert",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.fail_verify_no_cert"},
			want:   ".ssl.fail_verify_no_cert",
		},
		{
			name:   "external service with subset match ssl.fail_verify_no_cert",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.fail_verify_no_cert"},
			want:   ".ssl.fail_verify_no_cert",
		},
		{
			name:   "kubernetes service match ssl.fail_verify_error",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.fail_verify_error"},
			want:   ".ssl.fail_verify_error",
		},
		{
			name:   "kubernetes service with subset match ssl.fail_verify_error",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.fail_verify_error"},
			want:   ".ssl.fail_verify_error",
		},
		{
			name:   "external service match ssl.fail_verify_error",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.fail_verify_error"},
			want:   ".ssl.fail_verify_error",
		},
		{
			name:   "external service with subset match ssl.fail_verify_error",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.fail_verify_error"},
			want:   ".ssl.fail_verify_error",
		},
		{
			name:   "kubernetes service match ssl.fail_verify_san",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.fail_verify_san"},
			want:   ".ssl.fail_verify_san",
		},
		{
			name:   "kubernetes service with subset match ssl.fail_verify_san",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.fail_verify_san"},
			want:   ".ssl.fail_verify_san",
		},
		{
			name:   "external service match ssl.fail_verify_san",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.fail_verify_san"},
			want:   ".ssl.fail_verify_san",
		},
		{
			name:   "external service with subset match ssl.fail_verify_san",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.fail_verify_san"},
			want:   ".ssl.fail_verify_san",
		},
		{
			name:   "kubernetes service match ssl.fail_verify_cert_hash",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.fail_verify_cert_hash"},
			want:   ".ssl.fail_verify_cert_hash",
		},
		{
			name:   "kubernetes service with subset match ssl.fail_verify_cert_hash",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.fail_verify_cert_hash"},
			want:   ".ssl.fail_verify_cert_hash",
		},
		{
			name:   "external service match ssl.fail_verify_cert_hash",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.fail_verify_cert_hash"},
			want:   ".ssl.fail_verify_cert_hash",
		},
		{
			name:   "external service with subset match ssl.fail_verify_cert_hash",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.fail_verify_cert_hash"},
			want:   ".ssl.fail_verify_cert_hash",
		},
		{
			name:   "kubernetes service match ssl.ocsp_staple_failed",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.ocsp_staple_failed"},
			want:   ".ssl.ocsp_staple_failed",
		},
		{
			name:   "kubernetes service with subset match ssl.ocsp_staple_failed",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.ocsp_staple_failed"},
			want:   ".ssl.ocsp_staple_failed",
		},
		{
			name:   "external service match ssl.ocsp_staple_failed",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.ocsp_staple_failed"},
			want:   ".ssl.ocsp_staple_failed",
		},
		{
			name:   "external service with subset match ssl.ocsp_staple_failed",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.ocsp_staple_failed"},
			want:   ".ssl.ocsp_staple_failed",
		},
		{
			name:   "kubernetes service match ssl.ocsp_staple_omitted",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.ocsp_staple_omitted"},
			want:   ".ssl.ocsp_staple_omitted",
		},
		{
			name:   "kubernetes service with subset match ssl.ocsp_staple_omitted",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.ocsp_staple_omitted"},
			want:   ".ssl.ocsp_staple_omitted",
		},
		{
			name:   "external service match ssl.ocsp_staple_omitted",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.ocsp_staple_omitted"},
			want:   ".ssl.ocsp_staple_omitted",
		},
		{
			name:   "external service with subset match ssl.ocsp_staple_omitted",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.ocsp_staple_omitted"},
			want:   ".ssl.ocsp_staple_omitted",
		},
		{
			name:   "kubernetes service match ssl.ocsp_staple_responses",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.ocsp_staple_responses"},
			want:   ".ssl.ocsp_staple_responses",
		},
		{
			name:   "kubernetes service with subset match ssl.ocsp_staple_responses",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.ocsp_staple_responses"},
			want:   ".ssl.ocsp_staple_responses",
		},
		{
			name:   "external service match ssl.ocsp_staple_responses",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.ocsp_staple_responses"},
			want:   ".ssl.ocsp_staple_responses",
		},
		{
			name:   "external service with subset match ssl.ocsp_staple_responses",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.ocsp_staple_responses"},
			want:   ".ssl.ocsp_staple_responses",
		},
		{
			name:   "kubernetes service match ssl.ocsp_staple_requests",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ssl.ocsp_staple_requests"},
			want:   ".ssl.ocsp_staple_requests",
		},
		{
			name:   "kubernetes service with subset match ssl.ocsp_staple_requests",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ssl.ocsp_staple_requests"},
			want:   ".ssl.ocsp_staple_requests",
		},
		{
			name:   "external service match ssl.ocsp_staple_requests",
			fields: fields{input: "cluster.outbound|443||istio.io.ssl.ocsp_staple_requests"},
			want:   ".ssl.ocsp_staple_requests",
		},
		{
			name:   "external service with subset match ssl.ocsp_staple_requests",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ssl.ocsp_staple_requests"},
			want:   ".ssl.ocsp_staple_requests",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_tx_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_tx_segments"},
			want:   ".tcp_stats.cx_tx_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_tx_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_tx_segments"},
			want:   ".tcp_stats.cx_tx_segments",
		},
		{
			name:   "external service match tcp_stats.cx_tx_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_tx_segments"},
			want:   ".tcp_stats.cx_tx_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_tx_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_tx_segments"},
			want:   ".tcp_stats.cx_tx_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_rx_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_rx_segments"},
			want:   ".tcp_stats.cx_rx_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_rx_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_rx_segments"},
			want:   ".tcp_stats.cx_rx_segments",
		},
		{
			name:   "external service match tcp_stats.cx_rx_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_rx_segments"},
			want:   ".tcp_stats.cx_rx_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_rx_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_rx_segments"},
			want:   ".tcp_stats.cx_rx_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_tx_data_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_tx_data_segments"},
			want:   ".tcp_stats.cx_tx_data_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_tx_data_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_tx_data_segments"},
			want:   ".tcp_stats.cx_tx_data_segments",
		},
		{
			name:   "external service match tcp_stats.cx_tx_data_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_tx_data_segments"},
			want:   ".tcp_stats.cx_tx_data_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_tx_data_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_tx_data_segments"},
			want:   ".tcp_stats.cx_tx_data_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_rx_data_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_rx_data_segments"},
			want:   ".tcp_stats.cx_rx_data_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_rx_data_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_rx_data_segments"},
			want:   ".tcp_stats.cx_rx_data_segments",
		},
		{
			name:   "external service match tcp_stats.cx_rx_data_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_rx_data_segments"},
			want:   ".tcp_stats.cx_rx_data_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_rx_data_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_rx_data_segments"},
			want:   ".tcp_stats.cx_rx_data_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_tx_retransmitted_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_tx_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_retransmitted_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_tx_retransmitted_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_tx_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_retransmitted_segments",
		},
		{
			name:   "external service match tcp_stats.cx_tx_retransmitted_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_tx_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_retransmitted_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_tx_retransmitted_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_tx_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_retransmitted_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_tx_unsent_bytes",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_tx_unsent_bytes"},
			want:   ".tcp_stats.cx_tx_unsent_bytes",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_tx_unsent_bytes",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_tx_unsent_bytes"},
			want:   ".tcp_stats.cx_tx_unsent_bytes",
		},
		{
			name:   "external service match tcp_stats.cx_tx_unsent_bytes",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_tx_unsent_bytes"},
			want:   ".tcp_stats.cx_tx_unsent_bytes",
		},
		{
			name:   "external service with subset match tcp_stats.cx_tx_unsent_bytes",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_tx_unsent_bytes"},
			want:   ".tcp_stats.cx_tx_unsent_bytes",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_tx_unacked_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_tx_unacked_segments"},
			want:   ".tcp_stats.cx_tx_unacked_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_tx_unacked_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_tx_unacked_segments"},
			want:   ".tcp_stats.cx_tx_unacked_segments",
		},
		{
			name:   "external service match tcp_stats.cx_tx_unacked_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_tx_unacked_segments"},
			want:   ".tcp_stats.cx_tx_unacked_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_tx_unacked_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_tx_unacked_segments"},
			want:   ".tcp_stats.cx_tx_unacked_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_tx_percent_retransmitted_segments",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_tx_percent_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_percent_retransmitted_segments",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_tx_percent_retransmitted_segments",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_tx_percent_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_percent_retransmitted_segments",
		},
		{
			name:   "external service match tcp_stats.cx_tx_percent_retransmitted_segments",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_tx_percent_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_percent_retransmitted_segments",
		},
		{
			name:   "external service with subset match tcp_stats.cx_tx_percent_retransmitted_segments",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_tx_percent_retransmitted_segments"},
			want:   ".tcp_stats.cx_tx_percent_retransmitted_segments",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_rtt_us",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_rtt_us"},
			want:   ".tcp_stats.cx_rtt_us",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_rtt_us",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_rtt_us"},
			want:   ".tcp_stats.cx_rtt_us",
		},
		{
			name:   "external service match tcp_stats.cx_rtt_us",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_rtt_us"},
			want:   ".tcp_stats.cx_rtt_us",
		},
		{
			name:   "external service with subset match tcp_stats.cx_rtt_us",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_rtt_us"},
			want:   ".tcp_stats.cx_rtt_us",
		},
		{
			name:   "kubernetes service match tcp_stats.cx_rtt_variance_us",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.tcp_stats.cx_rtt_variance_us"},
			want:   ".tcp_stats.cx_rtt_variance_us",
		},
		{
			name:   "kubernetes service with subset match tcp_stats.cx_rtt_variance_us",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.tcp_stats.cx_rtt_variance_us"},
			want:   ".tcp_stats.cx_rtt_variance_us",
		},
		{
			name:   "external service match tcp_stats.cx_rtt_variance_us",
			fields: fields{input: "cluster.outbound|443||istio.io.tcp_stats.cx_rtt_variance_us"},
			want:   ".tcp_stats.cx_rtt_variance_us",
		},
		{
			name:   "external service with subset match tcp_stats.cx_rtt_variance_us",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.tcp_stats.cx_rtt_variance_us"},
			want:   ".tcp_stats.cx_rtt_variance_us",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_time"},
			want:   ".zone.a.b.upstream_rq_time",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_time",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_time"},
			want:   ".zone.a.b.upstream_rq_time",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_time"},
			want:   ".zone.a.b.upstream_rq_time",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_time",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_time"},
			want:   ".zone.a.b.upstream_rq_time",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_2xx"},
			want:   ".zone.a.b.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_2xx"},
			want:   ".zone.a.b.upstream_rq_2xx",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_2xx"},
			want:   ".zone.a.b.upstream_rq_2xx",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_2xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_2xx"},
			want:   ".zone.a.b.upstream_rq_2xx",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_3xx"},
			want:   ".zone.a.b.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_3xx"},
			want:   ".zone.a.b.upstream_rq_3xx",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_3xx"},
			want:   ".zone.a.b.upstream_rq_3xx",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_3xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_3xx"},
			want:   ".zone.a.b.upstream_rq_3xx",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_4xx"},
			want:   ".zone.a.b.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_4xx"},
			want:   ".zone.a.b.upstream_rq_4xx",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_4xx"},
			want:   ".zone.a.b.upstream_rq_4xx",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_4xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_4xx"},
			want:   ".zone.a.b.upstream_rq_4xx",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_5xx"},
			want:   ".zone.a.b.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_5xx"},
			want:   ".zone.a.b.upstream_rq_5xx",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_5xx"},
			want:   ".zone.a.b.upstream_rq_5xx",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_5xx",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_5xx"},
			want:   ".zone.a.b.upstream_rq_5xx",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_200"},
			want:   ".zone.a.b.upstream_rq_200",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_200",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_200"},
			want:   ".zone.a.b.upstream_rq_200",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_200"},
			want:   ".zone.a.b.upstream_rq_200",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_200",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_200"},
			want:   ".zone.a.b.upstream_rq_200",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_300"},
			want:   ".zone.a.b.upstream_rq_300",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_300",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_300"},
			want:   ".zone.a.b.upstream_rq_300",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_300"},
			want:   ".zone.a.b.upstream_rq_300",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_300",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_300"},
			want:   ".zone.a.b.upstream_rq_300",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_400"},
			want:   ".zone.a.b.upstream_rq_400",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_400",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_400"},
			want:   ".zone.a.b.upstream_rq_400",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_400"},
			want:   ".zone.a.b.upstream_rq_400",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_400",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_400"},
			want:   ".zone.a.b.upstream_rq_400",
		},
		{
			name:   "kubernetes service match zone.a.b.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.zone.a.b.upstream_rq_500"},
			want:   ".zone.a.b.upstream_rq_500",
		},
		{
			name:   "kubernetes service with subset match zone.a.b.upstream_rq_500",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.zone.a.b.upstream_rq_500"},
			want:   ".zone.a.b.upstream_rq_500",
		},
		{
			name:   "external service match zone.a.b.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443||istio.io.zone.a.b.upstream_rq_500"},
			want:   ".zone.a.b.upstream_rq_500",
		},
		{
			name:   "external service with subset match zone.a.b.upstream_rq_500",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.zone.a.b.upstream_rq_500"},
			want:   ".zone.a.b.upstream_rq_500",
		},
		{
			name:   "kubernetes service match lb_recalculate_zone_structures",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_recalculate_zone_structures"},
			want:   ".lb_recalculate_zone_structures",
		},
		{
			name:   "kubernetes service with subset match lb_recalculate_zone_structures",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_recalculate_zone_structures"},
			want:   ".lb_recalculate_zone_structures",
		},
		{
			name:   "external service match lb_recalculate_zone_structures",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_recalculate_zone_structures"},
			want:   ".lb_recalculate_zone_structures",
		},
		{
			name:   "external service with subset match lb_recalculate_zone_structures",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_recalculate_zone_structures"},
			want:   ".lb_recalculate_zone_structures",
		},
		{
			name:   "kubernetes service match lb_zone_cluster_too_small",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_zone_cluster_too_small"},
			want:   ".lb_zone_cluster_too_small",
		},
		{
			name:   "kubernetes service with subset match lb_zone_cluster_too_small",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_zone_cluster_too_small"},
			want:   ".lb_zone_cluster_too_small",
		},
		{
			name:   "external service match lb_zone_cluster_too_small",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_zone_cluster_too_small"},
			want:   ".lb_zone_cluster_too_small",
		},
		{
			name:   "external service with subset match lb_zone_cluster_too_small",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_zone_cluster_too_small"},
			want:   ".lb_zone_cluster_too_small",
		},
		{
			name:   "kubernetes service match lb_zone_routing_all_directly",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_zone_routing_all_directly"},
			want:   ".lb_zone_routing_all_directly",
		},
		{
			name:   "kubernetes service with subset match lb_zone_routing_all_directly",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_zone_routing_all_directly"},
			want:   ".lb_zone_routing_all_directly",
		},
		{
			name:   "external service match lb_zone_routing_all_directly",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_zone_routing_all_directly"},
			want:   ".lb_zone_routing_all_directly",
		},
		{
			name:   "external service with subset match lb_zone_routing_all_directly",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_zone_routing_all_directly"},
			want:   ".lb_zone_routing_all_directly",
		},
		{
			name:   "kubernetes service match lb_zone_routing_sampled",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_zone_routing_sampled"},
			want:   ".lb_zone_routing_sampled",
		},
		{
			name:   "kubernetes service with subset match lb_zone_routing_sampled",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_zone_routing_sampled"},
			want:   ".lb_zone_routing_sampled",
		},
		{
			name:   "external service match lb_zone_routing_sampled",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_zone_routing_sampled"},
			want:   ".lb_zone_routing_sampled",
		},
		{
			name:   "external service with subset match lb_zone_routing_sampled",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_zone_routing_sampled"},
			want:   ".lb_zone_routing_sampled",
		},
		{
			name:   "kubernetes service match lb_zone_routing_cross_zone",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_zone_routing_cross_zone"},
			want:   ".lb_zone_routing_cross_zone",
		},
		{
			name:   "kubernetes service with subset match lb_zone_routing_cross_zone",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_zone_routing_cross_zone"},
			want:   ".lb_zone_routing_cross_zone",
		},
		{
			name:   "external service match lb_zone_routing_cross_zone",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_zone_routing_cross_zone"},
			want:   ".lb_zone_routing_cross_zone",
		},
		{
			name:   "external service with subset match lb_zone_routing_cross_zone",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_zone_routing_cross_zone"},
			want:   ".lb_zone_routing_cross_zone",
		},
		{
			name:   "kubernetes service match lb_local_cluster_not_ok",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_local_cluster_not_ok"},
			want:   ".lb_local_cluster_not_ok",
		},
		{
			name:   "kubernetes service with subset match lb_local_cluster_not_ok",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_local_cluster_not_ok"},
			want:   ".lb_local_cluster_not_ok",
		},
		{
			name:   "external service match lb_local_cluster_not_ok",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_local_cluster_not_ok"},
			want:   ".lb_local_cluster_not_ok",
		},
		{
			name:   "external service with subset match lb_local_cluster_not_ok",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_local_cluster_not_ok"},
			want:   ".lb_local_cluster_not_ok",
		},
		{
			name:   "kubernetes service match lb_zone_number_differs",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_zone_number_differs"},
			want:   ".lb_zone_number_differs",
		},
		{
			name:   "kubernetes service with subset match lb_zone_number_differs",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_zone_number_differs"},
			want:   ".lb_zone_number_differs",
		},
		{
			name:   "external service match lb_zone_number_differs",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_zone_number_differs"},
			want:   ".lb_zone_number_differs",
		},
		{
			name:   "external service with subset match lb_zone_number_differs",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_zone_number_differs"},
			want:   ".lb_zone_number_differs",
		},
		{
			name:   "kubernetes service match lb_zone_no_capacity_left",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_zone_no_capacity_left"},
			want:   ".lb_zone_no_capacity_left",
		},
		{
			name:   "kubernetes service with subset match lb_zone_no_capacity_left",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_zone_no_capacity_left"},
			want:   ".lb_zone_no_capacity_left",
		},
		{
			name:   "external service match lb_zone_no_capacity_left",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_zone_no_capacity_left"},
			want:   ".lb_zone_no_capacity_left",
		},
		{
			name:   "external service with subset match lb_zone_no_capacity_left",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_zone_no_capacity_left"},
			want:   ".lb_zone_no_capacity_left",
		},
		{
			name:   "kubernetes service match original_dst_host_invalid",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.original_dst_host_invalid"},
			want:   ".original_dst_host_invalid",
		},
		{
			name:   "kubernetes service with subset match original_dst_host_invalid",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.original_dst_host_invalid"},
			want:   ".original_dst_host_invalid",
		},
		{
			name:   "external service match original_dst_host_invalid",
			fields: fields{input: "cluster.outbound|443||istio.io.original_dst_host_invalid"},
			want:   ".original_dst_host_invalid",
		},
		{
			name:   "external service with subset match original_dst_host_invalid",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.original_dst_host_invalid"},
			want:   ".original_dst_host_invalid",
		},
		{
			name:   "kubernetes service match lb_subsets_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_subsets_active"},
			want:   ".lb_subsets_active",
		},
		{
			name:   "kubernetes service with subset match lb_subsets_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_subsets_active"},
			want:   ".lb_subsets_active",
		},
		{
			name:   "external service match lb_subsets_active",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_subsets_active"},
			want:   ".lb_subsets_active",
		},
		{
			name:   "external service with subset match lb_subsets_active",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_subsets_active"},
			want:   ".lb_subsets_active",
		},
		{
			name:   "kubernetes service match lb_subsets_created",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_subsets_created"},
			want:   ".lb_subsets_created",
		},
		{
			name:   "kubernetes service with subset match lb_subsets_created",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_subsets_created"},
			want:   ".lb_subsets_created",
		},
		{
			name:   "external service match lb_subsets_created",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_subsets_created"},
			want:   ".lb_subsets_created",
		},
		{
			name:   "external service with subset match lb_subsets_created",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_subsets_created"},
			want:   ".lb_subsets_created",
		},
		{
			name:   "kubernetes service match lb_subsets_removed",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_subsets_removed"},
			want:   ".lb_subsets_removed",
		},
		{
			name:   "kubernetes service with subset match lb_subsets_removed",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_subsets_removed"},
			want:   ".lb_subsets_removed",
		},
		{
			name:   "external service match lb_subsets_removed",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_subsets_removed"},
			want:   ".lb_subsets_removed",
		},
		{
			name:   "external service with subset match lb_subsets_removed",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_subsets_removed"},
			want:   ".lb_subsets_removed",
		},
		{
			name:   "kubernetes service match lb_subsets_selected",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_subsets_selected"},
			want:   ".lb_subsets_selected",
		},
		{
			name:   "kubernetes service with subset match lb_subsets_selected",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_subsets_selected"},
			want:   ".lb_subsets_selected",
		},
		{
			name:   "external service match lb_subsets_selected",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_subsets_selected"},
			want:   ".lb_subsets_selected",
		},
		{
			name:   "external service with subset match lb_subsets_selected",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_subsets_selected"},
			want:   ".lb_subsets_selected",
		},
		{
			name:   "kubernetes service match lb_subsets_fallback",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_subsets_fallback"},
			want:   ".lb_subsets_fallback",
		},
		{
			name:   "kubernetes service with subset match lb_subsets_fallback",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_subsets_fallback"},
			want:   ".lb_subsets_fallback",
		},
		{
			name:   "external service match lb_subsets_fallback",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_subsets_fallback"},
			want:   ".lb_subsets_fallback",
		},
		{
			name:   "external service with subset match lb_subsets_fallback",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_subsets_fallback"},
			want:   ".lb_subsets_fallback",
		},
		{
			name:   "kubernetes service match lb_subsets_single_host_per_subset_duplicate",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.lb_subsets_single_host_per_subset_duplicate"},
			want:   ".lb_subsets_single_host_per_subset_duplicate",
		},
		{
			name:   "kubernetes service with subset match lb_subsets_single_host_per_subset_duplicate",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.lb_subsets_single_host_per_subset_duplicate"},
			want:   ".lb_subsets_single_host_per_subset_duplicate",
		},
		{
			name:   "external service match lb_subsets_single_host_per_subset_duplicate",
			fields: fields{input: "cluster.outbound|443||istio.io.lb_subsets_single_host_per_subset_duplicate"},
			want:   ".lb_subsets_single_host_per_subset_duplicate",
		},
		{
			name:   "external service with subset match lb_subsets_single_host_per_subset_duplicate",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.lb_subsets_single_host_per_subset_duplicate"},
			want:   ".lb_subsets_single_host_per_subset_duplicate",
		},
		{
			name:   "kubernetes service match ring_hash_lb.size",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ring_hash_lb.size"},
			want:   ".ring_hash_lb.size",
		},
		{
			name:   "kubernetes service with subset match ring_hash_lb.size",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ring_hash_lb.size"},
			want:   ".ring_hash_lb.size",
		},
		{
			name:   "external service match ring_hash_lb.size",
			fields: fields{input: "cluster.outbound|443||istio.io.ring_hash_lb.size"},
			want:   ".ring_hash_lb.size",
		},
		{
			name:   "external service with subset match ring_hash_lb.size",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ring_hash_lb.size"},
			want:   ".ring_hash_lb.size",
		},
		{
			name:   "kubernetes service match ring_hash_lb.min_hashes_per_host",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ring_hash_lb.min_hashes_per_host"},
			want:   ".ring_hash_lb.min_hashes_per_host",
		},
		{
			name:   "kubernetes service with subset match ring_hash_lb.min_hashes_per_host",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ring_hash_lb.min_hashes_per_host"},
			want:   ".ring_hash_lb.min_hashes_per_host",
		},
		{
			name:   "external service match ring_hash_lb.min_hashes_per_host",
			fields: fields{input: "cluster.outbound|443||istio.io.ring_hash_lb.min_hashes_per_host"},
			want:   ".ring_hash_lb.min_hashes_per_host",
		},
		{
			name:   "external service with subset match ring_hash_lb.min_hashes_per_host",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ring_hash_lb.min_hashes_per_host"},
			want:   ".ring_hash_lb.min_hashes_per_host",
		},
		{
			name:   "kubernetes service match ring_hash_lb.max_hashes_per_host",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.ring_hash_lb.max_hashes_per_host"},
			want:   ".ring_hash_lb.max_hashes_per_host",
		},
		{
			name:   "kubernetes service with subset match ring_hash_lb.max_hashes_per_host",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.ring_hash_lb.max_hashes_per_host"},
			want:   ".ring_hash_lb.max_hashes_per_host",
		},
		{
			name:   "external service match ring_hash_lb.max_hashes_per_host",
			fields: fields{input: "cluster.outbound|443||istio.io.ring_hash_lb.max_hashes_per_host"},
			want:   ".ring_hash_lb.max_hashes_per_host",
		},
		{
			name:   "external service with subset match ring_hash_lb.max_hashes_per_host",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.ring_hash_lb.max_hashes_per_host"},
			want:   ".ring_hash_lb.max_hashes_per_host",
		},
		{
			name:   "kubernetes service match maglev_lb.min_entries_per_host",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.maglev_lb.min_entries_per_host"},
			want:   ".maglev_lb.min_entries_per_host",
		},
		{
			name:   "kubernetes service with subset match maglev_lb.min_entries_per_host",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.maglev_lb.min_entries_per_host"},
			want:   ".maglev_lb.min_entries_per_host",
		},
		{
			name:   "external service match maglev_lb.min_entries_per_host",
			fields: fields{input: "cluster.outbound|443||istio.io.maglev_lb.min_entries_per_host"},
			want:   ".maglev_lb.min_entries_per_host",
		},
		{
			name:   "external service with subset match maglev_lb.min_entries_per_host",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.maglev_lb.min_entries_per_host"},
			want:   ".maglev_lb.min_entries_per_host",
		},
		{
			name:   "kubernetes service match maglev_lb.max_entries_per_host",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.maglev_lb.max_entries_per_host"},
			want:   ".maglev_lb.max_entries_per_host",
		},
		{
			name:   "kubernetes service with subset match maglev_lb.max_entries_per_host",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.maglev_lb.max_entries_per_host"},
			want:   ".maglev_lb.max_entries_per_host",
		},
		{
			name:   "external service match maglev_lb.max_entries_per_host",
			fields: fields{input: "cluster.outbound|443||istio.io.maglev_lb.max_entries_per_host"},
			want:   ".maglev_lb.max_entries_per_host",
		},
		{
			name:   "external service with subset match maglev_lb.max_entries_per_host",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.maglev_lb.max_entries_per_host"},
			want:   ".maglev_lb.max_entries_per_host",
		},
		{
			name:   "kubernetes service match upstream_rq_headers_size",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_headers_size"},
			want:   ".upstream_rq_headers_size",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_headers_size",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_headers_size"},
			want:   ".upstream_rq_headers_size",
		},
		{
			name:   "external service match upstream_rq_headers_size",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_headers_size"},
			want:   ".upstream_rq_headers_size",
		},
		{
			name:   "external service with subset match upstream_rq_headers_size",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_headers_size"},
			want:   ".upstream_rq_headers_size",
		},
		{
			name:   "kubernetes service match upstream_rq_body_size",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rq_body_size"},
			want:   ".upstream_rq_body_size",
		},
		{
			name:   "kubernetes service with subset match upstream_rq_body_size",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rq_body_size"},
			want:   ".upstream_rq_body_size",
		},
		{
			name:   "external service match upstream_rq_body_size",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rq_body_size"},
			want:   ".upstream_rq_body_size",
		},
		{
			name:   "external service with subset match upstream_rq_body_size",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rq_body_size"},
			want:   ".upstream_rq_body_size",
		},
		{
			name:   "kubernetes service match upstream_rs_headers_size",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rs_headers_size"},
			want:   ".upstream_rs_headers_size",
		},
		{
			name:   "kubernetes service with subset match upstream_rs_headers_size",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rs_headers_size"},
			want:   ".upstream_rs_headers_size",
		},
		{
			name:   "external service match upstream_rs_headers_size",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rs_headers_size"},
			want:   ".upstream_rs_headers_size",
		},
		{
			name:   "external service with subset match upstream_rs_headers_size",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rs_headers_size"},
			want:   ".upstream_rs_headers_size",
		},
		{
			name:   "kubernetes service match upstream_rs_body_size",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.cluster.local.upstream_rs_body_size"},
			want:   ".upstream_rs_body_size",
		},
		{
			name:   "kubernetes service with subset match upstream_rs_body_size",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.cluster.local.upstream_rs_body_size"},
			want:   ".upstream_rs_body_size",
		},
		{
			name:   "external service match upstream_rs_body_size",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_rs_body_size"},
			want:   ".upstream_rs_body_size",
		},
		{
			name:   "external service with subset match upstream_rs_body_size",
			fields: fields{input: "cluster.outbound|443|stable|istio.io.upstream_rs_body_size"},
			want:   ".upstream_rs_body_size",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, regex.FindString(test.fields.input))
		})
	}
}
