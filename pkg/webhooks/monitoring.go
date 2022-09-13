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

package webhooks

import (
	"istio.io/pkg/monitoring"
)

var (
	// webhookConfigNameTag holds the target webhook config name for the context.
	webhookConfigNameTag = monitoring.MustCreateLabel("name")

	// reasonTag holds the error reason for the context.
	reasonTag = monitoring.MustCreateLabel("reason")
)

var (
	metricWebhookPatchAttempts = monitoring.NewSum(
		"webhook_patch_attempts_total",
		"Webhook patching attempts",
		monitoring.WithLabels(webhookConfigNameTag),
	)

	metricWebhookPatchRetries = monitoring.NewSum(
		"webhook_patch_retries_total",
		"Webhook patching retries",
		monitoring.WithLabels(webhookConfigNameTag),
	)

	metricWebhookPatchFailures = monitoring.NewSum(
		"webhook_patch_failures_total",
		"Webhook patching total failures",
		monitoring.WithLabels(webhookConfigNameTag, reasonTag),
	)
)

const (
	// webhook patching failure reasons
	reasonWrongRevision         = "wrong_revision"
	reasonLoadCABundleFailure   = "load_ca_bundle_failure"
	reasonWebhookConfigNotFound = "webhook_config_not_found"
	reasonWebhookEntryNotFound  = "webhook_entry_not_found"
	reasonWebhookUpdateFailure  = "webhook_update_failure"
)

func init() {
	monitoring.MustRegister(
		metricWebhookPatchAttempts,
		metricWebhookPatchRetries,
		metricWebhookPatchFailures,
	)
}

func reportWebhookPatchAttempts(webhookConfigName string) {
	metricWebhookPatchAttempts.
		With(webhookConfigNameTag.Value(webhookConfigName)).
		Increment()
}

func reportWebhookPatchRetry(webhookConfigName string) {
	metricWebhookPatchRetries.
		With(webhookConfigNameTag.Value(webhookConfigName)).
		Increment()
}

func reportWebhookPatchFailure(webhookConfigName string, reason string) {
	metricWebhookPatchFailures.
		With(webhookConfigNameTag.Value(webhookConfigName)).
		With(reasonTag.Value(reason)).
		Increment()
}
