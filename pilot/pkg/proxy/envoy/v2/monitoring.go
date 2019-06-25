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
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"istio.io/istio/pilot/pkg/monitoring"
)

var (
	errTag, errTagErr         = tag.NewKey("err")
	clusterTag, clusterTagErr = tag.NewKey("cluster")
	nodeTag, nodeTagErr       = tag.NewKey("node")
	typeTag, typeTagErr       = tag.NewKey("type")

	cdsReject, cdsRejectView = monitoring.NewInt64AndView(
		"pilot_xds_cds_reject",
		"Pilot rejected CSD configs.",
		view.LastValue(),
		nodeTag, errTag)

	edsReject, edsRejectView = monitoring.NewInt64AndView(
		"pilot_xds_eds_reject",
		"Pilot rejected EDS.",
		view.LastValue(),
		nodeTag, errTag)

	edsInstances, edsInstancesView = monitoring.NewInt64AndView(
		"pilot_xds_eds_instances",
		"Instances for each cluster, as of last push. Zero instances is an error.",
		view.LastValue(),
		clusterTag)

	ldsReject, ldsRejectView = monitoring.NewInt64AndView(
		"pilot_xds_lds_reject",
		"Pilot rejected LDS.",
		view.LastValue(),
		nodeTag, errTag)

	rdsReject, rdsRejectView = monitoring.NewInt64AndView(
		"pilot_xds_rds_reject",
		"Pilot rejected RDS.",
		view.LastValue(),
		nodeTag, errTag)

	rdsExpiredNonce, rdsExpiredNonceView = monitoring.NewInt64AndView(
		"pilot_rds_expired_nonce",
		"Total number of RDS messages with an expired nonce.",
		view.Sum())

	totalXDSRejects, totalXDSRejectsView = monitoring.NewInt64AndView(
		"pilot_total_xds_rejects",
		"Total number of XDS responses from pilot rejected by proxy.",
		view.Sum())

	monServices, monServicesView = monitoring.NewInt64AndView(
		"pilot_services",
		"Total services known to pilot.",
		view.LastValue())

	// TODO: Update all the resource stats in separate routine
	// virtual services, destination rules, gateways, etc.
	monVServices, monVServicesView = monitoring.NewInt64AndView(
		"pilot_virt_services",
		"Total virtual services known to pilot.",
		view.LastValue())

	xdsClients, xdsClientsView = monitoring.NewInt64AndView(
		"pilot_xds",
		"Number of endpoints connected to this pilot using XDS.",
		view.LastValue())

	xdsResponseWriteTimeouts, xdsResponseWriteTimeoutsView = monitoring.NewInt64AndView(
		"pilot_xds_write_timeout",
		"Pilot XDS response write timeouts.",
		view.Sum())

	pushTimeouts, pushTimeoutsView = monitoring.NewInt64AndView(
		"pilot_xds_push_timeout",
		"Pilot push timeout, will retry.",
		view.Sum())

	pushTimeoutFailures, pushTimeoutFailuresView = monitoring.NewInt64AndView(
		"pilot_xds_push_timeout_failures",
		"Pilot push timeout failures after repeated attempts.",
		view.Sum())

	// Covers xds_builderr and xds_senderr for xds in {lds, rds, cds, eds}.
	pushes, pushesView = monitoring.NewInt64AndView(
		"pilot_xds_pushes",
		"Pilot build and send errors for lds, rds, cds and eds.",
		view.Sum(),
		typeTag)

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

	pushErrors, pushErrorsView = monitoring.NewInt64AndView(
		"pilot_xds_push_errors",
		"Number of errors (timeouts) pushing to sidecars.",
		view.Sum(),
		typeTag)

	unrecoverableErrs = pushErrors.WithTags(tag.Upsert(typeTag, "unrecoverable"))
	retryErrs         = pushErrors.WithTags(tag.Upsert(typeTag, "retry"))

	// only supported dimension is millis, unfortunately. default to unitdimensionless.
	proxiesConvergeDelay, proxiesConvergeDelayView = monitoring.NewFloat64AndView(
		"pilot_proxy_convergence_time",
		"Delay between config change and all proxies converging.",
		view.Distribution(1, 3, 5, 10, 20, 30, 50, 100),
	)

	pushContextErrors, pushContextErrorsView = monitoring.NewInt64AndView(
		"pilot_xds_push_context_errors",
		"Number of errors (timeouts) initiating push context.",
		view.Sum())

	totalXDSInternalErrors, totalXDSInternalErrorsView = monitoring.NewInt64AndView(
		"pilot_total_xds_internal_errors",
		"Total number of internal XDS errors in pilot.",
		view.Sum())

	inboundUpdates, inboundUpdatesView = monitoring.NewInt64AndView(
		"pilot_inbound_updates",
		"Total number of updates received by pilot.",
		view.Sum(),
		typeTag)

	inboundConfigUpdates   = inboundUpdates.WithTags(tag.Upsert(typeTag, "config"))
	inboundEDSUpdates      = inboundUpdates.WithTags(tag.Upsert(typeTag, "eds"))
	inboundServiceUpdates  = inboundUpdates.WithTags(tag.Upsert(typeTag, "svc"))
	inboundWorkloadUpdates = inboundUpdates.WithTags(tag.Upsert(typeTag, "workload"))
)

func incrementXDSRejects(stat *monitoring.Int64, node, errCode string) {
	stat.WithTags(tag.Upsert(nodeTag, node), tag.Upsert(errTag, errCode)).Increment()
	totalXDSRejects.Increment()
}

func init() {

	if errTagErr != nil {
		panic(errTagErr)
	}

	if clusterTagErr != nil {
		panic(clusterTagErr)
	}

	if nodeTagErr != nil {
		panic(nodeTagErr)
	}

	if typeTagErr != nil {
		panic(typeTagErr)
	}

	views := []*view.View{
		cdsRejectView,
		edsRejectView,
		ldsRejectView,
		rdsRejectView,
		edsInstancesView,
		rdsExpiredNonceView,
		totalXDSRejectsView,
		monServicesView,
		monVServicesView,
		xdsClientsView,
		xdsResponseWriteTimeoutsView,
		pushTimeoutsView,
		pushTimeoutFailuresView,
		pushesView,
		pushErrorsView,
		proxiesConvergeDelayView,
		pushContextErrorsView,
		totalXDSInternalErrorsView,
		inboundUpdatesView,
	}

	if err := view.Register(views...); err != nil {
		panic(err)
	}
}
