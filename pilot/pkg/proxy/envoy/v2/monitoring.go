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
	"go.opencensus.io/tag"

	"istio.io/istio/pilot/pkg/monitoring"
)

var (
	errTag     = monitoring.MustCreateTagKey("err")
	clusterTag = monitoring.MustCreateTagKey("cluster")
	nodeTag    = monitoring.MustCreateTagKey("node")
	typeTag    = monitoring.MustCreateTagKey("type")

	cdsReject = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_xds_cds_reject", "Pilot rejected CSD configs."},
		nodeTag, errTag,
	)

	edsReject = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_xds_eds_reject", "Pilot rejected EDS."},
		nodeTag, errTag,
	)

	edsInstances = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_xds_eds_instances", "Instances for each cluster, as of last push. Zero instances is an error."},
		clusterTag,
	)

	ldsReject = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_xds_lds_reject", "Pilot rejected LDS."},
		nodeTag, errTag,
	)

	rdsReject = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_xds_rds_reject", "Pilot rejected RDS."},
		nodeTag, errTag)

	rdsExpiredNonce = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_rds_expired_nonce", "Total number of RDS messages with an expired nonce."},
	)

	totalXDSRejects = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_total_xds_rejects", "Total number of XDS responses from pilot rejected by proxy."},
	)

	monServices = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_services", "Total services known to pilot."},
	)

	// TODO: Update all the resource stats in separate routine
	// virtual services, destination rules, gateways, etc.
	monVServices = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_virt_services", "Total virtual services known to pilot."},
	)

	xdsClients = monitoring.NewGauge(
		monitoring.MetricOpts{"pilot_xds", "Number of endpoints connected to this pilot using XDS."},
	)

	xdsResponseWriteTimeouts = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_xds_write_timeout", "Pilot XDS response write timeouts."},
	)

	pushTimeouts = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_xds_push_timeout", "Pilot push timeout, will retry."},
	)

	pushTimeoutFailures = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_xds_push_timeout_failures", "Pilot push timeout failures after repeated attempts."},
	)

	// Covers xds_builderr and xds_senderr for xds in {lds, rds, cds, eds}.
	pushes = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_xds_pushes", "Pilot build and send errors for lds, rds, cds and eds."},
		typeTag,
	)

	cdsPushes         = pushes.WithTags(tag.Upsert(typeTag, "cds"))
	cdsSendErrPushes  = pushes.WithTags(tag.Upsert(typeTag, "cds_senderr"))
	cdsBuildErrPushes = pushes.WithTags(tag.Upsert(typeTag, "cds_builderr"))
	edsPushes         = pushes.WithTags(tag.Upsert(typeTag, "eds"))
	edsSendErrPushes  = pushes.WithTags(tag.Upsert(typeTag, "eds_senderr"))
	ldsPushes         = pushes.WithTags(tag.Upsert(typeTag, "lds"))
	ldsSendErrPushes  = pushes.WithTags(tag.Upsert(typeTag, "lds_senderr"))
	ldsBuildErrPushes = pushes.WithTags(tag.Upsert(typeTag, "lds_builderr"))
	rdsPushes         = pushes.WithTags(tag.Upsert(typeTag, "rds"))
	rdsSendErrPushes  = pushes.WithTags(tag.Upsert(typeTag, "rds_senderr"))
	rdsBuildErrPushes = pushes.WithTags(tag.Upsert(typeTag, "rds_builderr"))

	pushErrors = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_xds_push_errors", "Number of errors (timeouts) pushing to sidecars."},
		typeTag,
	)

	unrecoverableErrs = pushErrors.WithTags(tag.Upsert(typeTag, "unrecoverable"))
	retryErrs         = pushErrors.WithTags(tag.Upsert(typeTag, "retry"))

	// only supported dimension is millis, unfortunately. default to unitdimensionless.
	proxiesConvergeDelay = monitoring.NewDistribution(
		monitoring.MetricOpts{"pilot_proxy_convergence_time", "Delay between config change and all proxies converging."},
		[]float64{1, 3, 5, 10, 20, 30, 50, 100},
	)

	pushContextErrors = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_xds_push_context_errors", "Number of errors (timeouts) initiating push context."},
	)

	totalXDSInternalErrors = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_total_xds_internal_errors", "Total number of internal XDS errors in pilot."},
	)

	inboundUpdates = monitoring.NewSum(
		monitoring.MetricOpts{"pilot_inbound_updates", "Total number of updates received by pilot."},
		typeTag,
	)

	inboundConfigUpdates   = inboundUpdates.WithTags(tag.Upsert(typeTag, "config"))
	inboundEDSUpdates      = inboundUpdates.WithTags(tag.Upsert(typeTag, "eds"))
	inboundServiceUpdates  = inboundUpdates.WithTags(tag.Upsert(typeTag, "svc"))
	inboundWorkloadUpdates = inboundUpdates.WithTags(tag.Upsert(typeTag, "workload"))
)

func incrementXDSRejects(metric monitoring.Metric, node, errCode string) {
	metric.WithTags(tag.Upsert(nodeTag, node), tag.Upsert(errTag, errCode)).Increment()
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
