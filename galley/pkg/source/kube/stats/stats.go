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

package stats

import (
	"context"
	"sync"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"istio.io/istio/galley/pkg/source/kube/log"
)

const (
	apiVersion = "apiVersion"
	group      = "group"
	kind       = "kind"
	errorStr   = "error"
	coreGroup  = "core"
)

var (
	// APIVersionTag holds the API version of the resource.
	APIVersionTag tag.Key
	// GroupTag holds the group of the resource.
	GroupTag tag.Key
	// KindTag holds the kind of the resource.
	KindTag tag.Key
	// ErrorTag holds the error message of a handleEvent failure.
	ErrorTag tag.Key
)

var (
	sourceEventError = stats.Int64(
		"galley/source/kube/event_error_total",
		"The number of times a kubernetes source encountered errored while handling an event",
		stats.UnitDimensionless)
	sourceEventSuccess = stats.Int64(
		"galley/source/kube/event_success_total",
		"The number of times a kubernetes source successfully handled an event",
		stats.UnitDimensionless)

	sourceConversionSuccess = stats.Int64(
		"galley/source/kube/dynamic/converter_success_total",
		"The number of times a dynamic kubernetes source successfully converted a resource",
		stats.UnitDimensionless)
	sourceConversionFailure = stats.Int64(
		"galley/source/kube/dynamic/converter_failure_total",
		"The number of times a dynamnic kubernetes source failed converting a resources",
		stats.UnitDimensionless)
)

// RecordEventError records an error handling a kube event.
func RecordEventError(msg string) {
	ctx, ctxErr := tag.New(context.Background(), tag.Insert(ErrorTag, msg))
	if ctxErr != nil {
		log.Scope.Errorf("error creating context to record handleEvent error")
	} else {
		stats.Record(ctx, sourceEventError.M(1))
	}
}

// RecordEventSuccess records successfully handling a kube event.
func RecordEventSuccess() {
	stats.Record(context.Background(), sourceEventSuccess.M(1))
}

type contextKey struct {
	apiVersion, group, kind string
}

var ctxCache = sync.Map{}

// RecordConverterResult records the result of a kube resource conversion from unstructured.
func RecordConverterResult(success bool, apiVersion, group, kind string) {
	var metric *stats.Int64Measure
	if success {
		metric = sourceConversionSuccess
	} else {
		metric = sourceConversionFailure
	}
	if len(group) == 0 {
		// For "core" resources, i.e. Pods and Nodes, group is "". Empty tags result in errors during the export to Prometheus.
		group = coreGroup
	}
	key := contextKey{apiVersion, group, kind}
	ctx, ok := ctxCache.Load(key)
	if !ok {
		var err error
		ctx, err = tag.New(context.Background(), tag.Insert(APIVersionTag, apiVersion),
			tag.Insert(GroupTag, group), tag.Insert(KindTag, kind))
		if err != nil {
			log.Scope.Errorf("Error creating monitoring context for counting conversion result: %v", err)
			return
		}
		ctxCache.Store(key, ctx)
	}
	stats.Record(ctx.(context.Context), metric.M(1))
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
		newView(sourceEventError, errorKey, view.Count()),
		newView(sourceEventSuccess, noKeys, view.Count()),
		newView(sourceConversionSuccess, conversionKeys, view.Count()),
		newView(sourceConversionFailure, conversionKeys, view.Count()),
	)

	if err != nil {
		panic(err)
	}
}
