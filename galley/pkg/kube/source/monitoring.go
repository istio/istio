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

package source

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/stats/view"
	"context"
)

const (
	apiVersion = "apiVersion"
	group = "group"
	kind = "kind"
	errorStr = "error"
)

var (
	APIVersionTag tag.Key
	GroupTag      tag.Key
	KindTag       tag.Key
	ErrorTag tag.Key
)

var (
	listenerHandleEventError = stats.Int64(
		"galley/kube/source/listener_handle_event_error_total",
		"The number of times the listener's handleEvent has errored",
		stats.UnitDimensionless)

	listenerHandleEventSuccess = stats.Int64(
		"galley/kube/source/listener_handle_event_success_total",
		"The number of times the listener's handleEvent has succeeded",
		stats.UnitDimensionless)

	sourceConversionSuccess = stats.Int64(
		"galley/kube/source/source_converter_success_total",
		"The number of times resource conversion in the source's ProcessEvent has succeeded",
		stats.UnitDimensionless)
	sourceConversionFailure = stats.Int64(
		"galley/kube/source/source_converter_failure_total",
		"The number of times resource conversion in the source's ProcessEvent has failed",
		stats.UnitDimensionless)
)

func recordHandleEventError(err string) {
	ctx, ctxErr := tag.New(context.Background(), tag.Insert(ErrorTag, err))
	if ctxErr != nil {
		scope.Errorf("error creating context to record handleEvent error")
	} else {
		stats.Record(ctx, listenerHandleEventError.M(1))
	}
}

func recordHandleEventSuccess() {
	stats.Record(context.Background(), listenerHandleEventSuccess.M(1))
}

type contextKey struct {
	apiVersion, group, kind string
}

var ctxCache = make(map[contextKey]context.Context)

func recordConverterResult(success bool, apiVersion, group, kind string) {
	var metric *stats.Int64Measure
	if success {
		metric = sourceConversionSuccess
	} else {
		metric = sourceConversionFailure
	}
	key := contextKey{apiVersion, group, kind}
	ctx, ok := ctxCache[key]
	var err error
	if !ok {
		ctx, err = tag.New(context.Background(), tag.Insert(APIVersionTag, apiVersion),
			tag.Insert(GroupTag, group), tag.Insert(KindTag, kind))
		if err != nil {
			scope.Errorf("Error creating monitoring context for counting conversion result: %v", err)
			return
		}
		ctxCache[key] = ctx
	}
	stats.Record(ctx, metric.M(1))
}

func newTagKey(label string) tag.Key {
	if t, err := tag.NewKey(label); err != nil {
		panic(err)
	} else {
		return t
	}
}

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
	APIVersionTag = newTagKey(apiVersion)
	GroupTag = newTagKey(group)
	KindTag = newTagKey(kind)
	ErrorTag = newTagKey(errorStr)

	errorKey := []tag.Key{ErrorTag}
	conversionKeys := []tag.Key{APIVersionTag, GroupTag, KindTag}
	var noKeys []tag.Key

	err := view.Register(
		newView(listenerHandleEventError, errorKey, view.Count()),
		newView(listenerHandleEventSuccess, noKeys, view.Count()),
		newView(sourceConversionSuccess, conversionKeys, view.Count()),
		newView(sourceConversionFailure, conversionKeys, view.Count()),
	)

	if err != nil {
		panic(err)
	}
}
