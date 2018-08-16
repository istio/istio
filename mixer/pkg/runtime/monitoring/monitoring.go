// Copyright 2018 Istio Authors
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
	configID     = "configID"
	initConfigID = "initConfigID" // the id of the config, at which the adapter was instantiated.
	handler      = "handler"
	meshFunction = "meshFunction"
	adapterName  = "adapter"
	errorStr     = "error"
)

var (
	// Tags
	ConfigIDTag, InitConfigIDTag, HandlerTag, MeshFunctionTag, AdapterTag, ErrorTag tag.Key

	// distribution buckets
	durationBuckets = []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	countBuckets    = []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 15, 20}

	// config measures
	AttributesTotal           = stats.Int64("mixer/config/attributes_total", "The number of known attributes in the current config.", stats.UnitDimensionless)
	HandlersTotal             = stats.Int64("mixer/config/handler_configs_total", "The number of known handlers in the current config.", stats.UnitDimensionless)
	InstancesTotal            = stats.Int64("mixer/config/instance_configs_total", "The number of known instances in the current config.", stats.UnitDimensionless)
	InstanceErrs              = stats.Int64("mixer/config/instance_config_errors_total", "The number of errors encountered during processing of the instance configuration.", stats.UnitDimensionless)
	RulesTotal                = stats.Int64("mixer/config/rule_configs_total", "The number of known rules in the current config.", stats.UnitDimensionless)
	RuleErrs                  = stats.Int64("mixer/config/rule_config_errors_total", "The number of errors encountered during processing of the rule configuration.", stats.UnitDimensionless)
	AdapterInfosTotal         = stats.Int64("mixer/config/adapter_info_configs_total", "The number of known adapters in the current config.", stats.UnitDimensionless)
	AdapterErrs               = stats.Int64("mixer/config/adapter_info_config_errors_total", "The number of errors encountered during processing of the adapter info configuration.", stats.UnitDimensionless)
	TemplatesTotal            = stats.Int64("mixer/config/template_configs_total", "The number of known templates in the current config.", stats.UnitDimensionless)
	TemplateErrs              = stats.Int64("mixer/config/template_config_errors_total", "The number of errors encountered during processing of the template configuration.", stats.UnitDimensionless)
	MatchErrors               = stats.Int64("mixer/config/rule_config_match_error_total", "The number of rule conditions that was not parseable.", stats.UnitDimensionless)
	UnsatisfiedActionHandlers = stats.Int64("mixer/config/unsatisfied_action_handler_total", "The number of actions that failed due to handlers being unavailable.", stats.UnitDimensionless)
	HandlerValidationErrors   = stats.Int64("mixer/config/handler_validation_error_total", "The number of errors encountered because handler validation returned error.", stats.UnitDimensionless)

	// handler measures
	NewHandlersTotal    = stats.Int64("mixer/handler/new_handlers_total", "The number of handlers that were newly created during config transition.", stats.UnitDimensionless)
	ReusedHandlersTotal = stats.Int64("mixer/handler/reused_handlers_total", "The number of handlers that were re-used during config transition.", stats.UnitDimensionless)
	ClosedHandlersTotal = stats.Int64("mixer/handler/closed_handlers_total", "The number of handlers that were closed during config transition.", stats.UnitDimensionless)
	BuildFailuresTotal  = stats.Int64("mixer/handler/handler_build_failures_total", "The number of handlers that failed creation during config transition.", stats.UnitDimensionless)
	CloseFailuresTotal  = stats.Int64("mixer/handler/handler_close_failures_total", "The number of errors encountered while closing handlers during config transition.", stats.UnitDimensionless)

	WorkersTotal = stats.Int64("mixer/handler/workers_total", "The current number of active worker routines in a given adapter environment.", stats.UnitDimensionless)
	DaemonsTotal = stats.Int64("mixer/handler/daemons_total", "The current number of active daemon routines in a given adapter environment.", stats.UnitDimensionless)

	// dispatch(er) measures
	DispatchesTotal = stats.Int64("mixer/runtime/dispatches_total", "Total number of adapter dispatches handled by Mixer.", stats.UnitDimensionless)
	// TODO: would OC support unit of Seconds ?
	DispatchDurationsSeconds = stats.Float64("mixer/runtime/dispatch_duration_seconds", "Duration in seconds for adapter dispatches handled by Mixer.", stats.UnitDimensionless)
	DestinationsPerRequest   = stats.Int64("mixer/dispatcher/destinations_per_request", "Number of handlers dispatched per request by Mixer", stats.UnitDimensionless)
	InstancesPerRequest      = stats.Int64("mixer/dispatcher/instances_per_request", "Number of handlers dispatched per request by Mixer", stats.UnitDimensionless)
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
	if ConfigIDTag, err = tag.NewKey(configID); err != nil {
		panic(err)
	}
	if InitConfigIDTag, err = tag.NewKey(initConfigID); err != nil {
		panic(err)
	}
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

	configKeys := []tag.Key{ConfigIDTag}
	envConfigKeys := []tag.Key{InitConfigIDTag, HandlerTag}
	dispatchKeys := []tag.Key{MeshFunctionTag, HandlerTag, AdapterTag, ErrorTag}

	runtimeViews := []*view.View{
		// config views
		newView(AttributesTotal, configKeys, view.Count()),
		newView(HandlersTotal, configKeys, view.Count()),
		newView(InstancesTotal, configKeys, view.Count()),
		newView(InstanceErrs, configKeys, view.Count()),
		newView(RulesTotal, configKeys, view.Count()),
		newView(RuleErrs, configKeys, view.Count()),
		newView(AdapterInfosTotal, configKeys, view.Count()),
		newView(AdapterErrs, configKeys, view.Count()),
		newView(TemplatesTotal, configKeys, view.Count()),
		newView(TemplateErrs, configKeys, view.Count()),
		newView(MatchErrors, configKeys, view.Count()),
		newView(UnsatisfiedActionHandlers, configKeys, view.Count()),
		newView(HandlerValidationErrors, configKeys, view.Count()),
		newView(NewHandlersTotal, configKeys, view.Count()),
		newView(ReusedHandlersTotal, configKeys, view.Count()),
		newView(ClosedHandlersTotal, configKeys, view.Count()),
		newView(BuildFailuresTotal, configKeys, view.Count()),
		newView(CloseFailuresTotal, configKeys, view.Count()),

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
