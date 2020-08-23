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

package gcpmonitoring

import (
	"context"
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

var (
	typeHookTag     = tag.MustNewKey("type")
	eventHookTag    = tag.MustNewKey("event")
	resourceHookTag = tag.MustNewKey("resource")
	versionHookTag  = tag.MustNewKey("version")

	pilotK8sCfgEvents            = "pilot_k8s_cfg_events"
	pilotK8sRegEvents            = "pilot_k8s_reg_events"
	galleyValidationPassed       = "galley/validation/passed"
	galleyValidationFailed       = "galley/validation/failed"
	pilotXDSPushes               = "pilot_xds_pushes"
	pilotXDSEDSReject            = "pilot_xds_eds_reject"
	pilotXDSRDSReject            = "pilot_xds_rds_reject"
	pilotXDSLDSReject            = "pilot_xds_lds_reject"
	pilotXDSCDSReject            = "pilot_xds_cds_reject"
	pilotProxyConvergenceTime    = "pilot_proxy_convergence_time"
	pilotXDS                     = "pilot_xds"
	sidecarInjectionSuccessTotal = "sidecar_injection_success_total"
	sidecarInjectionFailureTotal = "sidecar_injection_failure_total"
	sidecarInjectionSkipTotal    = "sidecar_injection_skip_total"
)

type gcpRecordHook struct{}

var _ monitoring.RecordHook = &gcpRecordHook{}

func (r gcpRecordHook) OnRecordFloat64Measure(f *stats.Float64Measure, tags []tag.Mutator, value float64) {
	switch f.Name() {
	case pilotK8sCfgEvents, pilotK8sRegEvents:
		onPilotK8sCfgEvents(f, tags, value)
	case galleyValidationPassed:
		onGalleyValidationPass(f, tags, value)
	case galleyValidationFailed:
		onGalleyValidationFailed(f, tags, value)
	case pilotXDSPushes:
		onPilotXDSPushes(f, tags, value)
	case pilotXDSEDSReject, pilotXDSRDSReject, pilotXDSLDSReject, pilotXDSCDSReject:
		onPilotXDSReject(f, tags, value)
	case pilotProxyConvergenceTime:
		onPilotConfigConvergence(f, tags, value)
	case pilotXDS:
		onPilotXDS(f, tags, value)
	case sidecarInjectionSuccessTotal, sidecarInjectionFailureTotal, sidecarInjectionSkipTotal:
		onSidecarInjection(f, tags, value)
	}
}

func registerHook() {
	hook := gcpRecordHook{}
	monitoring.RegisterRecordHook(pilotK8sCfgEvents, hook)
	monitoring.RegisterRecordHook(pilotK8sRegEvents, hook)
	monitoring.RegisterRecordHook(galleyValidationPassed, hook)
	monitoring.RegisterRecordHook(galleyValidationFailed, hook)
	monitoring.RegisterRecordHook(pilotXDSPushes, hook)
	monitoring.RegisterRecordHook(pilotXDSEDSReject, hook)
	monitoring.RegisterRecordHook(pilotXDSRDSReject, hook)
	monitoring.RegisterRecordHook(pilotXDSLDSReject, hook)
	monitoring.RegisterRecordHook(pilotXDSCDSReject, hook)
	monitoring.RegisterRecordHook(pilotProxyConvergenceTime, hook)
	monitoring.RegisterRecordHook(pilotXDS, hook)
	monitoring.RegisterRecordHook(sidecarInjectionSuccessTotal, hook)
	monitoring.RegisterRecordHook(sidecarInjectionFailureTotal, hook)
	monitoring.RegisterRecordHook(sidecarInjectionSkipTotal, hook)
}

func onPilotK8sCfgEvents(_ *stats.Float64Measure, tags []tag.Mutator, value float64) {
	tm := getOriginalTagMap(tags)
	if tm == nil {
		return
	}
	t, found := tm.Value(typeHookTag)
	if !found {
		return
	}
	e, found := tm.Value(eventHookTag)
	if !found {
		return
	}
	ctx, err := tag.New(context.Background(), tag.Insert(operationKey, e), tag.Insert(typeKey, t))
	if err != nil {
		return
	}
	stats.Record(ctx, configEventMeasure.M(int64(value)))
}

func onGalleyValidationPass(_ *stats.Float64Measure, tags []tag.Mutator, value float64) {
	tm := getOriginalTagMap(tags)
	if tm == nil {
		return
	}
	res, found := tm.Value(resourceHookTag)
	if !found {
		return
	}
	ctx, err := tag.New(context.Background(), tag.Insert(typeKey, res), tag.Insert(successKey, "true"))
	if err != nil {
		return
	}
	stats.Record(ctx, configValidationMeasuare.M(int64(value)))
}

func onGalleyValidationFailed(_ *stats.Float64Measure, tags []tag.Mutator, value float64) {
	tm := getOriginalTagMap(tags)
	if tm == nil {
		return
	}
	res, found := tm.Value(resourceHookTag)
	if !found {
		return
	}
	ctx, err := tag.New(context.Background(), tag.Insert(typeKey, res), tag.Insert(successKey, "false"))
	if err != nil {
		return
	}
	stats.Record(ctx, configValidationMeasuare.M(int64(value)))
}

func onPilotXDSPushes(_ *stats.Float64Measure, tags []tag.Mutator, value float64) {
	tm := getOriginalTagMap(tags)
	if tm == nil {
		return
	}
	t, found := tm.Value(typeHookTag)
	if !found || len(t) < 3 {
		return
	}
	xdsType := strings.ToUpper(t[0:3])
	status := "true"
	if len(t) > 3 {
		status = "false"
	}

	ctx, err := tag.New(context.Background(), tag.Insert(typeKey, xdsType), tag.Insert(successKey, status))
	if err != nil {
		return
	}
	stats.Record(ctx, configPushMeasuare.M(int64(value)))
}

func onPilotXDSReject(f *stats.Float64Measure, _ []tag.Mutator, value float64) {
	// measure name is patterned as pilot_xds_xxx_reject, where xxx is the xds type.
	xdsType := strings.ToUpper(f.Name()[10:13])
	ctx, err := tag.New(context.Background(), tag.Insert(typeKey, xdsType))
	if err != nil {
		return
	}
	stats.Record(ctx, rejectedConfigMeasuare.M(int64(value)))
}

func onPilotConfigConvergence(_ *stats.Float64Measure, _ []tag.Mutator, value float64) {
	stats.Record(context.Background(), configConvergenceMeasuare.M(value))
}

func onPilotXDS(_ *stats.Float64Measure, tags []tag.Mutator, value float64) {
	tm := getOriginalTagMap(tags)
	if tm == nil {
		return
	}
	res, found := tm.Value(versionHookTag)
	if !found {
		return
	}
	ctx, err := tag.New(context.Background(), tag.Insert(proxyVersionKey, res))
	if err != nil {
		return
	}
	stats.Record(ctx, proxyClientsMeasure.M(int64(value)))
}

func onSidecarInjection(f *stats.Float64Measure, _ []tag.Mutator, value float64) {
	status := ""
	switch f.Name() {
	case sidecarInjectionSuccessTotal:
		status = "true"
	case sidecarInjectionFailureTotal:
		status = "false"
	default:
		return
	}
	ctx, err := tag.New(context.Background(), tag.Insert(successKey, status))
	if err != nil {
		return
	}
	stats.Record(ctx, sidecarInjectionMeasure.M(int64(value)))
}

func getOriginalTagMap(tags []tag.Mutator) *tag.Map {
	originalCtx, err := tag.New(context.Background(), tags...)
	if err != nil {
		log.Warn("fail to initialize original tag context")
		return nil
	}
	return tag.FromContext(originalCtx)
}
