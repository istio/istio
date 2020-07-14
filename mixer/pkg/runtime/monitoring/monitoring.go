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

package monitoring

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	// tag names used by runtime packages
	handler      = "handler"
	meshFunction = "meshFunction"
	adapterName  = "adapter"
	errorStr     = "error"
	varietyStr   = "variety"
)

var (
	// HandlerTag holds the current handler for the context.
	HandlerTag tag.Key
	// MeshFunctionTag holds the current mesh function (logentry, metric, etc) for the context.
	MeshFunctionTag tag.Key
	// AdapterTag holds the current adapter for the context.
	AdapterTag tag.Key
	// ErrorTag holds the current error for the context.
	ErrorTag tag.Key
	// VarietyTag holds the template variety
	VarietyTag tag.Key

	// distribution buckets
	durationBuckets = []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	countBuckets    = []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 15, 20}

	// AttributesTotal is a measure of the number of known attributes.
	AttributesTotal = stats.Int64(
		"mixer/config/attributes_total",
		"The number of known attributes in the current config.",
		stats.UnitDimensionless)

	// HandlersTotal is a measure of the number of known handlers.
	HandlersTotal = stats.Int64(
		"mixer/config/handler_configs_total",
		"The number of known handlers in the current config.",
		stats.UnitDimensionless)

	// InstancesTotal is a measure of the number of known instances.
	InstancesTotal = stats.Int64(
		"mixer/config/instance_configs_total",
		"The number of known instances in the current config.",
		stats.UnitDimensionless)

	// InstanceErrs is a measure of the number of errors for processing instance config.
	InstanceErrs = stats.Int64(
		"mixer/config/instance_config_errors_total",
		"The number of errors encountered during processing of the instance configuration.",
		stats.UnitDimensionless)

	// RulesTotal is a measure of the number of known rules.
	RulesTotal = stats.Int64(
		"mixer/config/rule_configs_total",
		"The number of known rules in the current config.",
		stats.UnitDimensionless)

	// RuleErrs is a measure of the number of errors for processing rules config.
	RuleErrs = stats.Int64(
		"mixer/config/rule_config_errors_total",
		"The number of errors encountered during processing of the rule configuration.",
		stats.UnitDimensionless)

	// AdapterInfosTotal is a measure of the number of known adapters.
	AdapterInfosTotal = stats.Int64(
		"mixer/config/adapter_info_configs_total",
		"The number of known adapters in the current config.",
		stats.UnitDimensionless)

	// AdapterErrs is a measure of the number of errors for processing adapter config.
	AdapterErrs = stats.Int64(
		"mixer/config/adapter_info_config_errors_total",
		"The number of errors encountered during processing of the adapter info configuration.",
		stats.UnitDimensionless)

	// TemplatesTotal is a measure of the number of known templates.
	TemplatesTotal = stats.Int64(
		"mixer/config/template_configs_total",
		"The number of known templates in the current config.",
		stats.UnitDimensionless)

	// TemplateErrs is a measure of the number of errors for processing template config.
	TemplateErrs = stats.Int64(
		"mixer/config/template_config_errors_total",
		"The number of errors encountered during processing of the template configuration.",
		stats.UnitDimensionless)

	// MatchErrors is a measure of the number of errors for processing rule conditions.
	MatchErrors = stats.Int64(
		"mixer/config/rule_config_match_error_total",
		"The number of rule conditions that was not parseable.",
		stats.UnitDimensionless)

	// UnsatisfiedActionHandlers is a measure of the number of actions that failed due to missing handlers.
	UnsatisfiedActionHandlers = stats.Int64(
		"mixer/config/unsatisfied_action_handler_total",
		"The number of actions that failed due to handlers being unavailable.",
		stats.UnitDimensionless)

	// HandlerValidationErrors is a measure of the number of errors validating handler config.
	HandlerValidationErrors = stats.Int64(
		"mixer/config/handler_validation_error_total",
		"The number of errors encountered because handler validation returned error.",
		stats.UnitDimensionless)

	// NewHandlersTotal is a measure of the number of handlers newly-created during config processing.
	NewHandlersTotal = stats.Int64(
		"mixer/handler/new_handlers_total",
		"The number of handlers that were newly created during config transition.",
		stats.UnitDimensionless)

	// ReusedHandlersTotal is a measure of the number of handlers reused during config processing.
	ReusedHandlersTotal = stats.Int64(
		"mixer/handler/reused_handlers_total",
		"The number of handlers that were re-used during config transition.",
		stats.UnitDimensionless)

	// ClosedHandlersTotal is a measure of the number of handlers closed during config processing.
	ClosedHandlersTotal = stats.Int64(
		"mixer/handler/closed_handlers_total",
		"The number of handlers that were closed during config transition.",
		stats.UnitDimensionless)

	// BuildFailuresTotal is a measure of the number of errors building handlers during config processing.
	BuildFailuresTotal = stats.Int64(
		"mixer/handler/handler_build_failures_total",
		"The number of handlers that failed creation during config transition.",
		stats.UnitDimensionless)

	// CloseFailuresTotal is a measure of the number of errors closing handlers during config processing.
	CloseFailuresTotal = stats.Int64(
		"mixer/handler/handler_close_failures_total",
		"The number of errors encountered while closing handlers during config transition.",
		stats.UnitDimensionless)

	// WorkersTotal is a measure of the number of active worker go-routines for a handler.
	WorkersTotal = stats.Int64(
		"mixer/handler/workers_total",
		"The current number of active worker routines in a given adapter environment.",
		stats.UnitDimensionless)

	// DaemonsTotal is a measure of the number of active daemon go-routines for a handler.
	DaemonsTotal = stats.Int64(
		"mixer/handler/daemons_total",
		"The current number of active daemon routines in a given adapter environment.",
		stats.UnitDimensionless)

	// DispatchesTotal is a measure of the number of handler dispatches.
	DispatchesTotal = stats.Int64(
		"mixer/runtime/dispatches_total",
		"Total number of adapter dispatches handled by Mixer.",
		stats.UnitDimensionless)

	// DispatchDurationsSeconds is a measure of the number of seconds spent in dispatch.
	DispatchDurationsSeconds = stats.Float64(
		"mixer/runtime/dispatch_duration_seconds",
		"Duration in seconds for adapter dispatches handled by Mixer.",
		stats.UnitDimensionless)

	// DestinationsPerRequest is a measure of the number of handlers dispatched per request.
	DestinationsPerRequest = stats.Int64(
		"mixer/dispatcher/destinations_per_request",
		"Number of handlers dispatched per request by Mixer",
		stats.UnitDimensionless)

	// InstancesPerRequest is a measure of the number of instances created per request.
	InstancesPerRequest = stats.Int64(
		"mixer/dispatcher/instances_per_request",
		"Number of instances created per request by Mixer",
		stats.UnitDimensionless)

	DestinationsPerVarietyTotal = stats.Int64(
		"mixer/dispatcher/destinations_per_variety_total",
		"Number of Mixer adapter destinations by template variety type",
		stats.UnitDimensionless)
)

func newView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) *view.View {
	return &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure,
		TagKeys:     keys,
		Aggregation: aggregation,
	}
}

func init() {
	var err error
	if MeshFunctionTag, err = tag.NewKey(meshFunction); err != nil {
		panic(err)
	}
	if HandlerTag, err = tag.NewKey(handler); err != nil {
		panic(err)
	}
	if AdapterTag, err = tag.NewKey(adapterName); err != nil {
		panic(err)
	}
	if ErrorTag, err = tag.NewKey(errorStr); err != nil {
		panic(err)
	}
	if VarietyTag, err = tag.NewKey(varietyStr); err != nil {
		panic(err)
	}

	envConfigKeys := []tag.Key{HandlerTag}
	dispatchKeys := []tag.Key{MeshFunctionTag, HandlerTag, AdapterTag, ErrorTag}
	varietyKeys := []tag.Key{VarietyTag}

	runtimeViews := []*view.View{
		// config views
		newView(AttributesTotal, []tag.Key{}, view.LastValue()),
		newView(HandlersTotal, []tag.Key{}, view.LastValue()),
		newView(InstancesTotal, []tag.Key{}, view.LastValue()),
		newView(InstanceErrs, []tag.Key{}, view.LastValue()),
		newView(RulesTotal, []tag.Key{}, view.LastValue()),
		newView(RuleErrs, []tag.Key{}, view.LastValue()),
		newView(AdapterInfosTotal, []tag.Key{}, view.LastValue()),
		newView(AdapterErrs, []tag.Key{}, view.LastValue()),
		newView(TemplatesTotal, []tag.Key{}, view.LastValue()),
		newView(TemplateErrs, []tag.Key{}, view.LastValue()),
		newView(MatchErrors, []tag.Key{}, view.LastValue()),
		newView(UnsatisfiedActionHandlers, []tag.Key{}, view.LastValue()),
		newView(HandlerValidationErrors, []tag.Key{}, view.LastValue()),
		newView(NewHandlersTotal, []tag.Key{}, view.LastValue()),
		newView(ReusedHandlersTotal, []tag.Key{}, view.LastValue()),
		newView(ClosedHandlersTotal, []tag.Key{}, view.LastValue()),
		newView(BuildFailuresTotal, []tag.Key{}, view.LastValue()),
		newView(CloseFailuresTotal, []tag.Key{}, view.LastValue()),
		newView(DestinationsPerVarietyTotal, varietyKeys, view.LastValue()),

		// env views
		newView(WorkersTotal, envConfigKeys, view.LastValue()),
		newView(DaemonsTotal, envConfigKeys, view.LastValue()),

		// dispatch views
		newView(DispatchesTotal, dispatchKeys, view.Count()),
		newView(DispatchDurationsSeconds, dispatchKeys, view.Distribution(durationBuckets...)),

		// others
		newView(DestinationsPerRequest, []tag.Key{}, view.Distribution(countBuckets...)),
		newView(InstancesPerRequest, []tag.Key{}, view.Distribution(countBuckets...)),
	}

	if err = view.Register(runtimeViews...); err != nil {
		panic(err)
	}
}
