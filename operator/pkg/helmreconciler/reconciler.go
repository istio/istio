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

package helmreconciler

import (
	"time"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
)

// Options are options for HelmReconciler.
type Options struct {
	// DryRun executes all actions but does not write anything to the cluster.
	DryRun bool
	// Log is a console logger for user visible CLI output.
	Log clog.Logger
	// Wait determines if we will wait for resources to be fully applied. Only applies to components that have no
	// dependencies.
	Wait bool
	// WaitTimeout controls the amount of time to wait for resources in a component to become ready before giving up.
	WaitTimeout time.Duration
	// Log tracks the installation progress for all components.
	ProgressLog *progress.Log
	// Force ignores validation errors
	Force bool
	// SkipPrune will skip pruning
	SkipPrune bool
}

type ProcessDefaultWebhookOptions struct {
	Namespace string
	DryRun    bool
}

func DetectIfTagWebhookIsNeeded(iop *v1alpha1.IstioOperator, exists bool) bool {
	rev := iop.Spec.Revision
	isDefaultInstallation := rev == "" && iop.Spec.Components.Pilot != nil && iop.Spec.Components.Pilot.Enabled.Value
	operatorManageWebhooks := operatorManageWebhooks(iop)
	return !operatorManageWebhooks && (!exists || isDefaultInstallation)
}

// operatorManageWebhooks returns .Values.global.operatorManageWebhooks from the Istio Operator.
func operatorManageWebhooks(iop *v1alpha1.IstioOperator) bool {
	if iop.Spec.GetValues() == nil {
		return false
	}
	globalValues := iop.Spec.Values.AsMap()["global"]
	global, ok := globalValues.(map[string]any)
	if !ok {
		return false
	}
	omw, ok := global["operatorManageWebhooks"].(bool)
	if !ok {
		return false
	}
	return omw
}
