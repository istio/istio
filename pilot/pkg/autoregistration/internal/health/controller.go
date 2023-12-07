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

package health

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/autoregistration/internal/state"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

type HealthEvent struct {
	// whether or not the agent thought the target is healthy
	Healthy bool `json:"healthy,omitempty"`
	// error message propagated
	Message string `json:"errMessage,omitempty"`
}

type HealthCondition struct {
	proxy     *model.Proxy
	entryName string
	condition *v1alpha1.IstioCondition
}

// Controller knows how to update health status of a workload.
type Controller struct {
	stateStore *state.Store

	// healthCondition is a fifo queue used for updating health check status
	healthCondition controllers.Queue
}

// NewController returns a new Controller instance.
func NewController(stateStore *state.Store, maxRetries int) *Controller {
	c := &Controller{
		stateStore: stateStore,
	}
	c.healthCondition = controllers.NewQueue("healthcheck",
		controllers.WithMaxAttempts(maxRetries),
		controllers.WithGenericReconciler(c.updateWorkloadEntryHealth))
	return c
}

func (c *Controller) Run(stop <-chan struct{}) {
	c.healthCondition.Run(stop)
}

// QueueWorkloadEntryHealth enqueues the associated WorkloadEntries health status.
func (c *Controller) QueueWorkloadEntryHealth(proxy *model.Proxy, event HealthEvent) {
	// we assume that the workload entry exists
	// if auto registration does not exist, try looking
	// up in NodeMetadata
	entryName, _ := proxy.WorkloadEntry()
	if entryName == "" {
		log.Errorf("unable to derive WorkloadEntry for health update for %v", proxy.ID)
		return
	}

	condition := transformHealthEvent(proxy, entryName, event)
	c.healthCondition.Add(condition)
}

func transformHealthEvent(proxy *model.Proxy, entryName string, event HealthEvent) HealthCondition {
	cond := &v1alpha1.IstioCondition{
		Type: status.ConditionHealthy,
		// last probe and transition are the same because
		// we only send on transition in the agent
		LastProbeTime:      timestamppb.Now(),
		LastTransitionTime: timestamppb.Now(),
	}
	out := HealthCondition{
		proxy:     proxy,
		entryName: entryName,
		condition: cond,
	}
	if event.Healthy {
		cond.Status = status.StatusTrue
		return out
	}
	cond.Status = status.StatusFalse
	cond.Message = event.Message
	return out
}

// updateWorkloadEntryHealth updates the associated WorkloadEntries health status
// based on the corresponding health check performed by istio-agent.
func (c *Controller) updateWorkloadEntryHealth(obj any) error {
	condition := obj.(HealthCondition)
	return c.stateStore.UpdateHealth(condition.proxy.ID, condition.entryName, condition.proxy.Metadata.Namespace, condition.condition)
}
