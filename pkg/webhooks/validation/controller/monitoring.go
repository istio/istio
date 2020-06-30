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

package controller

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	kubeMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	reason = "reason"
)

var (
	// reasonTag holds the error reason for the context.
	reasonTag tag.Key
)

var (
	metricWebhookConfigurationUpdateError = stats.Int64(
		"galley/validation/config_update_error",
		"k8s webhook configuration update error",
		stats.UnitDimensionless)
	metricWebhookConfigurationUpdates = stats.Int64(
		"galley/validation_config_updates",
		"k8s webhook configuration updates",
		stats.UnitDimensionless)
	metricWebhookConfigurationDeleteError = stats.Int64(
		"galley/validation/config_delete_error",
		"k8s webhook configuration delete error",
		stats.UnitDimensionless)
	metricWebhookConfigurationLoadError = stats.Int64(
		"galley/validation/config_load_error",
		"k8s webhook configuration (re)load error",
		stats.UnitDimensionless)
	metricWebhookConfigurationLoad = stats.Int64(
		"galley/validation/config_load",
		"k8s webhook configuration (re)loads",
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
	if reasonTag, err = tag.NewKey(reason); err != nil {
		panic(err)
	}

	var noKeys []tag.Key
	reasonKey := []tag.Key{reasonTag}

	err = view.Register(
		newView(metricWebhookConfigurationUpdateError, reasonKey, view.Count()),
		newView(metricWebhookConfigurationUpdates, noKeys, view.Count()),
		newView(metricWebhookConfigurationDeleteError, reasonKey, view.Count()),
		newView(metricWebhookConfigurationLoadError, reasonKey, view.Count()),
		newView(metricWebhookConfigurationLoad, noKeys, view.Count()),
	)

	if err != nil {
		panic(err)
	}
}

func reportValidationConfigUpdateError(reason kubeMeta.StatusReason) {
	ctx, err := tag.New(context.Background(), tag.Insert(reasonTag, string(reason)))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationConfigUpdateError: %v", err)
	} else {
		stats.Record(ctx, metricWebhookConfigurationUpdateError.M(1))
	}
}

func reportValidationConfigLoadError(reason string) {
	ctx, err := tag.New(context.Background(), tag.Insert(reasonTag, reason))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationConfigLoadError: %v", err)
	} else {
		stats.Record(ctx, metricWebhookConfigurationLoadError.M(1))
	}
}

func reportValidationConfigUpdate() {
	stats.Record(context.Background(), metricWebhookConfigurationUpdates.M(1))
}
