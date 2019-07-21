// Copyright 2019 Istio Authors
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
package v2

import (
	"github.com/gogo/status"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pilot/pkg/monitoring"
)

var (
	errTag     = monitoring.MustCreateTag("err")
	clusterTag = monitoring.MustCreateTag("cluster")
	nodeTag    = monitoring.MustCreateTag("node")
	typeTag    = monitoring.MustCreateTag("type")

	cdsReject = monitoring.NewGauge(
		"pilot_xds_cds_reject",
		"Pilot rejected CSD configs.",
		nodeTag, errTag,
	)

	edsReject = monitoring.NewGauge(
		"pilot_xds_eds_reject",
		"Pilot rejected EDS.",
		nodeTag, errTag,
	)

	edsInstances = monitoring.NewGauge(
		"pilot_xds_eds_instances",
		"Instances for each cluster, as of last push. Zero instances is an error.",
		clusterTag,
	)

	ldsReject = monitoring.NewGauge(
		"pilot_xds_lds_reject",
		"Pilot rejected LDS.",
		nodeTag, errTag,
	)

	rdsReject = monitoring.NewGauge(
		"pilot_xds_rds_reject",
		"Pilot rejected RDS.",
		nodeTag, errTag,
	)

	rdsExpiredNonce = monitoring.NewSum(
		"pilot_rds_expired_nonce",
		"Total number of RDS messages with an expired nonce.",
	)

	totalXDSRejects = monitoring.NewSum(
		"pilot_total_xds_rejects",
		"Total number of XDS responses from pilot rejected by proxy.",
	)

	monServices = monitoring.NewGauge(
		"pilot_services",
		"Total services known to pilot.",
	)

	// TODO: Update all the resource stats in separate routine
	// virtual services, destination rules, gateways, etc.
	monVServices = monitoring.NewGauge(
		"pilot_virt_services",
		"Total virtual services known to pilot.",
	)

	xdsClients = monitoring.NewGauge(
		"pilot_xds",
		"Number of endpoints connected to this pilot using XDS.",
	)

	xdsResponseWriteTimeouts = monitoring.NewSum(
		"pilot_xds_write_timeout",
		"Pilot XDS response write timeouts.",
	)

	pushTimeouts = monitoring.NewSum(
		"pilot_xds_push_timeout",
		"Pilot push timeout, will retry.",
	)

	pushTimeoutFailures = monitoring.NewSum(
		"pilot_xds_push_timeout_failures",
		"Pilot push timeout failures after repeated attempts.",
	)

	// Covers xds_builderr and xds_senderr for xds in {lds, rds, cds, eds}.
	pushes = monitoring.NewSum(
		"pilot_xds_pushes",
		"Pilot build and send errors for lds, rds, cds and eds.",
		typeTag,
	)

	cdsPushes         = pushes.With(typeTag.Value("cds"))
	cdsSendErrPushes  = pushes.With(typeTag.Value("cds_senderr"))
	cdsBuildErrPushes = pushes.With(typeTag.Value("cds_builderr"))
	edsPushes         = pushes.With(typeTag.Value("eds"))
	edsSendErrPushes  = pushes.With(typeTag.Value("eds_senderr"))
	ldsPushes         = pushes.With(typeTag.Value("lds"))
	ldsSendErrPushes  = pushes.With(typeTag.Value("lds_senderr"))
	ldsBuildErrPushes = pushes.With(typeTag.Value("lds_builderr"))
	rdsPushes         = pushes.With(typeTag.Value("rds"))
	rdsSendErrPushes  = pushes.With(typeTag.Value("rds_senderr"))
	rdsBuildErrPushes = pushes.With(typeTag.Value("rds_builderr"))

	pushErrors = monitoring.NewSum(
		"pilot_xds_push_errors",
		"Number of errors (timeouts) pushing to sidecars.",
		typeTag,
	)

	unrecoverableErrs = pushErrors.With(typeTag.Value("unrecoverable"))
	retryErrs         = pushErrors.With(typeTag.Value("retry"))

	// only supported dimension is millis, unfortunately. default to unitdimensionless.
	proxiesConvergeDelay = monitoring.NewDistribution(
		"pilot_proxy_convergence_time",
		"Delay between config change and all proxies converging.",
		[]float64{1, 3, 5, 10, 20, 30, 50, 100},
	)

	pushContextErrors = monitoring.NewSum(
		"pilot_xds_push_context_errors",
		"Number of errors (timeouts) initiating push context.",
	)

	totalXDSInternalErrors = monitoring.NewSum(
		"pilot_total_xds_internal_errors",
		"Total number of internal XDS errors in pilot.",
	)

	inboundUpdates = monitoring.NewSum(
		"pilot_inbound_updates",
		"Total number of updates received by pilot.",
		typeTag,
	)

	inboundConfigUpdates   = inboundUpdates.With(typeTag.Value("config"))
	inboundEDSUpdates      = inboundUpdates.With(typeTag.Value("eds"))
	inboundServiceUpdates  = inboundUpdates.With(typeTag.Value("svc"))
	inboundWorkloadUpdates = inboundUpdates.With(typeTag.Value("workload"))
)

func recordSendError(metric monitoring.Metric, err error) {
	s, ok := status.FromError(err)
	// Unavailable code will be sent when a connection is closing down. This is very normal,
	// due to the XDS connection being dropped every 30 minutes, or a pod shutting down.
	if !ok || s.Code() != codes.Unavailable {
		metric.Increment()
	}
}

func incrementXDSRejects(metric monitoring.Metric, node, errCode string) {
	metric.With(nodeTag.Value(node), errTag.Value(errCode)).Increment()
	totalXDSRejects.Increment()
}

func init() {
	monitoring.MustRegisterViews(
		cdsReject,
		edsReject,
		ldsReject,
		rdsReject,
		edsInstances,
		rdsExpiredNonce,
		totalXDSRejects,
		monServices,
		monVServices,
		xdsClients,
		xdsResponseWriteTimeouts,
		pushTimeouts,
		pushTimeoutFailures,
		pushes,
		pushErrors,
		proxiesConvergeDelay,
		pushContextErrors,
		totalXDSInternalErrors,
		inboundUpdates,
	)
}
