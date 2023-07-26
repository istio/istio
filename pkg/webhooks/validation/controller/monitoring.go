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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/monitoring"
)

var (
	// reasonLabel describes reason
	reasonLabel = monitoring.CreateLabel("reason")

	metricWebhookConfigurationUpdateError = monitoring.NewSum(
		"galley_validation_config_update_error",
		"k8s webhook configuration update error",
	)
	metricWebhookConfigurationUpdates = monitoring.NewSum(
		"galley_validation_config_updates",
		"k8s webhook configuration updates")
	metricWebhookConfigurationLoadError = monitoring.NewSum(
		"galley_validation_config_load_error",
		"k8s webhook configuration (re)load error",
	)
)

func reportValidationConfigUpdateError(reason metav1.StatusReason) {
	metricWebhookConfigurationUpdateError.With(reasonLabel.Value(string(reason))).Increment()
}

func reportValidationConfigLoadError(reason string) {
	metricWebhookConfigurationLoadError.With(reasonLabel.Value(reason)).Increment()
}

func reportValidationConfigUpdate() {
	metricWebhookConfigurationUpdates.Increment()
}
