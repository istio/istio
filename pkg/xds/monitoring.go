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

package xds

import (
	"time"

	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/monitoring"
)

var (
	Log = istiolog.RegisterScope("ads", "ads debugging")
	log = Log

	errTag  = monitoring.CreateLabel("err")
	nodeTag = monitoring.CreateLabel("node")
	typeTag = monitoring.CreateLabel("type")

	TotalXDSInternalErrors = monitoring.NewSum(
		"pilot_total_xds_internal_errors",
		"Total number of internal XDS errors in pilot.",
	)

	ExpiredNonce = monitoring.NewSum(
		"pilot_xds_expired_nonce",
		"Total number of XDS requests with an expired nonce.",
	)

	// pilot_total_xds_rejects should be used instead. This is for backwards compatibility
	cdsReject = monitoring.NewGauge(
		"pilot_xds_cds_reject",
		"Pilot rejected CDS configs.",
	)

	// pilot_total_xds_rejects should be used instead. This is for backwards compatibility
	edsReject = monitoring.NewGauge(
		"pilot_xds_eds_reject",
		"Pilot rejected EDS.",
	)

	// pilot_total_xds_rejects should be used instead. This is for backwards compatibility
	ldsReject = monitoring.NewGauge(
		"pilot_xds_lds_reject",
		"Pilot rejected LDS.",
	)

	// pilot_total_xds_rejects should be used instead. This is for backwards compatibility
	rdsReject = monitoring.NewGauge(
		"pilot_xds_rds_reject",
		"Pilot rejected RDS.",
	)

	totalXDSRejects = monitoring.NewSum(
		"pilot_total_xds_rejects",
		"Total number of XDS responses from pilot rejected by proxy.",
	)

	ResponseWriteTimeouts = monitoring.NewSum(
		"pilot_xds_write_timeout",
		"Pilot XDS response write timeouts.",
	)

	// Maximum side XDS request received so far.
	// This is useful for monitoring that we have not exceeded the hard gRPC recv limit (defaults 4mb).
	maxXdsRecv = monitoring.NewGauge(
		"pilot_xds_recv_max",
		"The maximum size request we have received so far.",
	)

	sendTime = monitoring.NewDistribution(
		"pilot_xds_send_time",
		"Total time in seconds Pilot takes to send generated configuration.",
		[]float64{.01, .1, 1, 3, 5, 10, 20, 30},
	)
)

func RecordRecvSize(s int64) {
	maxXdsRecv.RecordInt(s)
}

func IncrementXDSRejects(xdsType string, node, errCode string) {
	totalXDSRejects.With(typeTag.Value(model.GetMetricType(xdsType))).Increment()
	switch xdsType {
	case model.ListenerType:
		ldsReject.With(nodeTag.Value(node), errTag.Value(errCode)).Increment()
	case model.ClusterType:
		cdsReject.With(nodeTag.Value(node), errTag.Value(errCode)).Increment()
	case model.EndpointType:
		edsReject.With(nodeTag.Value(node), errTag.Value(errCode)).Increment()
	case model.RouteType:
		rdsReject.With(nodeTag.Value(node), errTag.Value(errCode)).Increment()
	}
}

func RecordSendTime(duration time.Duration) {
	sendTime.Record(duration.Seconds())
}
