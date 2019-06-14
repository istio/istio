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
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	nodeTag    tag.Key
	errTag     tag.Key
	clusterTag tag.Key
	typeTag    tag.Key

	// experiment on getting some monitoring on config errors.
	cdsReject       = stats.Int64("pilot_xds_cds_reject", "Pilot rejected CSD configs.", stats.UnitDimensionless)
	edsReject       = stats.Int64("pilot_xds_eds_reject", "Pilot rejected EDS.", stats.UnitDimensionless)
	edsInstances    = stats.Int64("pilot_xds_eds_instances", "Instances for each cluster, as of last push. Zero instances is an error.", stats.UnitDimensionless)
	ldsReject       = stats.Int64("pilot_xds_lds_reject", "Pilot rejected LDS.", stats.UnitDimensionless)
	rdsReject       = stats.Int64("pilot_xds_rds_reject", "Pilot rejected RDS.", stats.UnitDimensionless)
	rdsExpiredNonce = stats.Int64("pilot_rds_expired_nonce", "Total number of RDS messages with an expired nonce.", stats.UnitDimensionless)
	totalXDSRejects = stats.Int64("pilot_total_xds_rejects", "Total number of XDS responses from pilot rejected by proxy.", stats.UnitDimensionless)
	monServices     = stats.Int64("pilot_services", "Total services known to pilot.", stats.UnitDimensionless)

	// TODO: Update all the resource stats in separate routine
	// virtual services, destination rules, gateways, etc.
	monVServices             = stats.Int64("pilot_virt_services", "Total virtual services known to pilot.", stats.UnitDimensionless)
	xdsClients               = stats.Int64("pilot_xds", "Number of endpoints connected to this pilot using XDS.", stats.UnitDimensionless)
	xdsResponseWriteTimeouts = stats.Int64("pilot_xds_write_timeout", "Pilot XDS response write timeouts.", stats.UnitDimensionless)
	pushTimeouts             = stats.Int64("pilot_xds_push_timeout", "Pilot push timeout, will retry.", stats.UnitDimensionless)
	pushTimeoutFailures      = stats.Int64("pilot_xds_push_timeout_failures", "Pilot push timeout failures after repeated attempts.", stats.UnitDimensionless)

	// Covers xds_builderr and xds_senderr for xds in {lds, rds, cds, eds}.
	pushes             = stats.Int64("pilot_xds_pushes", "Pilot build and send errors for lds, rds, cds and eds.", stats.UnitDimensionless)
	cdsPushCtx         = typeContext("cds")
	cdsSendErrPushCtx  = typeContext("cds_senderr")
	cdsBuildErrPushCtx = typeContext("cds_builderr")
	edsPushCtx         = typeContext("eds")
	edsSendErrPushCtx  = typeContext("eds_senderr")
	edsBuildErrPushCtx = typeContext("eds_builderr")
	ldsPushCtx         = typeContext("lds")
	ldsSendErrPushCtx  = typeContext("lds_senderr")
	ldsBuildErrPushCtx = typeContext("lds_builderr")
	rdsPushCtx         = typeContext("rds")
	rdsSendErrPushCtx  = typeContext("rds_senderr")
	rdsBuildErrPushCtx = typeContext("rds_builderr")

	pushErrors       = stats.Int64("pilot_xds_push_errors", "Number of errors (timeouts) pushing to sidecars.", stats.UnitDimensionless)
	unrecoverableCtx = typeContext("unrecoverable")
	retryCtx         = typeContext("retry")

	// only supported dimension is millis, unfortunately. default to unitdimensionless.
	proxiesConvergeDelay     = stats.Float64("pilot_proxy_convergence_time", "Delay between config change and all proxies converging.", stats.UnitDimensionless)
	proxiesConvergenceBounds = view.Distribution(1, 3, 5, 10, 20, 30, 50, 100)
	pushContextErrors        = stats.Int64("pilot_xds_push_context_errors", "Number of errors (timeouts) initiating push context.", stats.UnitDimensionless)
	totalXDSInternalErrors   = stats.Int64("pilot_total_xds_internal_errors", "Total number of internal XDS errors in pilot (check logs).", stats.UnitDimensionless)

	inboundUpdates     = stats.Int64("pilot_inbound_updates", "Total number of updates received by pilot.", stats.UnitDimensionless)
	configUpdatesCtx   = typeContext("config")
	edsUpdatesCtx      = typeContext("eds")
	svcUpdatesCtx      = typeContext("svc")
	workloadUpdatesCtx = typeContext("workload")
)

func increment(measures ...stats.Measure) {
	incrementWith(context.Background(), measures...)
}

func incrementWith(ctx context.Context, measures ...stats.Measure) {
	for _, m := range measures {
		switch v := m.(type) {
		case *stats.Int64Measure:
			recordWith(ctx, v, 1)
		case *stats.Float64Measure:
			recordFloatWith(ctx, v, 1)
		}
	}
}

func recordFloat(measure *stats.Float64Measure, value float64) {
	recordFloatWith(context.Background(), measure, value)
}

func recordFloatWith(ctx context.Context, measure *stats.Float64Measure, value float64) {
	stats.Record(ctx, measure.M(value))
}

func record(measure *stats.Int64Measure, value int64) {
	recordWith(context.Background(), measure, value)
}

func recordWith(ctx context.Context, measure *stats.Int64Measure, value int64) {
	stats.Record(ctx, measure.M(value))
}

func incrementXDSRejects(measure *stats.Int64Measure, node, errCode string) {
	ctx, _ := tag.New(context.Background(), tag.Upsert(nodeTag, node), tag.Upsert(errTag, errCode))
	incrementWith(ctx, measure, totalXDSRejects)
}

func typeContext(kind string) context.Context {
	ctx, _ := tag.New(context.Background(), tag.Upsert(typeTag, kind))
	return ctx
}

func clusterContext(cluster string) context.Context {
	ctx, _ := tag.New(context.Background(), tag.Upsert(clusterTag, cluster))
	return ctx
}

func init() {
	var err error
	if typeTag, err = tag.NewKey("type"); err != nil {
		panic(err)
	}
	if errTag, err = tag.NewKey("err"); err != nil {
		panic(err)
	}
	if clusterTag, err = tag.NewKey("cluster"); err != nil {
		panic(err)
	}
	if nodeTag, err = tag.NewKey("node"); err != nil {
		panic(err)
	}
	xdsTagKeys := []tag.Key{nodeTag, errTag}
	clusterTagKeys := []tag.Key{clusterTag}
	typeTagKeys := []tag.Key{typeTag}

	if err := view.Register(
		&view.View{Measure: cdsReject, TagKeys: xdsTagKeys, Aggregation: view.LastValue()},
		&view.View{Measure: edsReject, TagKeys: xdsTagKeys, Aggregation: view.LastValue()},
		&view.View{Measure: ldsReject, TagKeys: xdsTagKeys, Aggregation: view.LastValue()},
		&view.View{Measure: rdsReject, TagKeys: xdsTagKeys, Aggregation: view.LastValue()},
		&view.View{Measure: edsInstances, TagKeys: clusterTagKeys, Aggregation: view.LastValue()},

		&view.View{Measure: rdsExpiredNonce, Aggregation: view.Sum()},
		&view.View{Measure: totalXDSRejects, Aggregation: view.Sum()},

		&view.View{Measure: monServices, Aggregation: view.LastValue()},
		&view.View{Measure: monVServices, Aggregation: view.LastValue()},
		&view.View{Measure: xdsClients, Aggregation: view.LastValue()},
		&view.View{Measure: xdsResponseWriteTimeouts, Aggregation: view.Sum()},
		&view.View{Measure: pushTimeouts, Aggregation: view.Sum()},
		&view.View{Measure: pushTimeoutFailures, Aggregation: view.Sum()},

		&view.View{Measure: pushes, TagKeys: typeTagKeys, Aggregation: view.Sum()},
		&view.View{Measure: pushErrors, TagKeys: typeTagKeys, Aggregation: view.Sum()},
		&view.View{Measure: inboundUpdates, TagKeys: typeTagKeys, Aggregation: view.Sum()},

		&view.View{Measure: pushContextErrors, Aggregation: view.Sum()},
		&view.View{Measure: totalXDSInternalErrors, Aggregation: view.Sum()},

		&view.View{Measure: proxiesConvergeDelay, Aggregation: proxiesConvergenceBounds},
	); err != nil {
		panic(err)
	}
}
