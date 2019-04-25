// Copyright 2018 The Operator-SDK Authors
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

package fake

import (
	"fmt"
	"time"

	"github.com/operator-framework/operator-sdk/pkg/ansible/runner"
	"github.com/operator-framework/operator-sdk/pkg/ansible/runner/eventapi"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Runner - implements the Runner interface for a GVK that's being watched.
type Runner struct {
	Finalizer                   string
	ReconcilePeriod             time.Duration
	ManageStatus                bool
	WatchDependentResources     bool
	WatchClusterScopedResources bool
	// Used to send error if Run should fail.
	Error error
	// Job Events that will be sent back from the runs channel
	JobEvents []eventapi.JobEvent
	//Stdout standard out to reply if failure occurs.
	Stdout string
}

type runResult struct {
	events <-chan eventapi.JobEvent
	stdout string
}

func (r *runResult) Events() <-chan eventapi.JobEvent {
	return r.events
}

func (r *runResult) Stdout() (string, error) {
	if r.stdout != "" {
		return r.stdout, nil
	}
	return r.stdout, fmt.Errorf("unable to find standard out")
}

// Run - runs the fake runner.
func (r *Runner) Run(_ string, u *unstructured.Unstructured, _ string) (runner.RunResult, error) {
	if r.Error != nil {
		return nil, r.Error
	}
	c := make(chan eventapi.JobEvent)
	go func() {
		for _, je := range r.JobEvents {
			c <- je
		}
		close(c)
	}()
	return &runResult{events: c, stdout: r.Stdout}, nil
}

// GetReconcilePeriod - new reconcile period.
func (r *Runner) GetReconcilePeriod() (time.Duration, bool) {
	return r.ReconcilePeriod, r.ReconcilePeriod != time.Duration(0)
}

// GetManageStatus - get managestatus.
func (r *Runner) GetManageStatus() bool {
	return r.ManageStatus
}

// GetWatchDependentResources - get watchDependentResources.
func (r *Runner) GetWatchDependentResources() bool {
	return r.WatchDependentResources
}

// GetWatchClusterScopedResources - get watchClusterScopedResources.
func (r *Runner) GetWatchClusterScopedResources() bool {
	return r.WatchClusterScopedResources
}

// GetFinalizer - gets the fake finalizer.
func (r *Runner) GetFinalizer() (string, bool) {
	return r.Finalizer, r.Finalizer != ""
}
