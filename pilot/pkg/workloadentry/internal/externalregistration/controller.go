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

package externalregistration

import (
	"fmt"
	"time"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/workloadentry/internal/autoregistration"
	"istio.io/istio/pilot/pkg/workloadentry/internal/health"
	"istio.io/istio/pilot/pkg/workloadentry/internal/state"
	"istio.io/istio/pkg/config"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

// Controller manages lifecycle of those workloads that are not using auto-registration.
type Controller struct {
	stateStore *state.Store
	cb         ControllerCallbacks
}

// ControllerCallbacks represents a contract between a Controller and
// a autoregistration.Controller.
type ControllerCallbacks interface {
	// GetWorkloadEntry return a WorkloadEntry by key.
	GetWorkloadEntry(entryName, entryNs string) *config.Config
	// ChangeWorkloadEntryStateToConnected updates given WorkloadEntry to reflect that
	// it is now connected to this particular `istiod` instance.
	ChangeWorkloadEntryStateToConnected(entryName, entryNs string, conTime time.Time) (changed bool, err error)
	// ChangeWorkloadEntryStateToDisconnected updates given WorkloadEntry to reflect that
	// it is no longer connected to this particular `istiod` instance.
	ChangeWorkloadEntryStateToDisconnected(entryName, entryNs string, disconTime, origConTime time.Time) (changed bool, err error)
	// IsExpired returns true if a given WorkloadEntry is eligible for cleanup.
	IsExpired(wle *config.Config, cleanupGracePeriod time.Duration) bool
	// IsControlled returns true if a given WorkloadEntry is/was connected to one of the `istiod` instances.
	IsControlled(wle *config.Config) bool
}

// NewController returns a new Controller instance.
func NewController(stateStore *state.Store, cb ControllerCallbacks) *Controller {
	return &Controller{
		stateStore: stateStore,
		cb:         cb,
	}
}

// IsApplicableTo returns true if health-checking is enabled for a given Istio Proxy.
func (c *Controller) IsApplicableTo(proxy *model.Proxy) bool {
	return features.WorkloadEntryHealthChecks && proxy.Metadata.WorkloadEntry != ""
}

// GetWorkloadEntryName returns a name of a WorkloadEntry resource that corresponds to a given
// Istio Proxy, or an empty string if health status updates are not supported by that WorkloadEntry.
func (c *Controller) GetWorkloadEntryName(proxy *model.Proxy) (string, error) {
	entryName, entryNs := proxy.Metadata.WorkloadEntry, proxy.Metadata.Namespace
	// a non-empty value of the `WorkloadEntry` field indicates that proxy must correspond to the WorkloadEntry
	wle := c.cb.GetWorkloadEntry(entryName, entryNs)
	if wle == nil {
		// either invalid proxy configuration or config propagation delay
		return "", fmt.Errorf("proxy metadata indicates that it must correspond to an existing WorkloadEntry, "+
			"however WorkloadEntry %s/%s is not found", entryNs, entryName)
	}
	if !health.IsEligibleForHealthStatusUpdates(wle) {
		return "", nil
	}
	return wle.Name, nil
}

// OnWorkloadConnect implements WorkloadEntryRegistrationStrategy.
//
// Updates WorkloadEntry on workload connect.
func (c *Controller) OnWorkloadConnect(entryName, entryNs string, proxy *model.Proxy, conTime time.Time) error {
	changed, err := c.cb.ChangeWorkloadEntryStateToConnected(entryName, entryNs, conTime)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	log.Infof("updated health-checked WorkloadEntry %s/%s", entryNs, entryName)
	return nil
}

// OnWorkloadDisconnect implements WorkloadEntryRegistrationStrategy.
//
// Updates WorkloadEntry on workload disconnect.
func (c *Controller) OnWorkloadDisconnect(entryName, entryNs string, disconTime, origConTime time.Time) (bool, error) {
	return c.cb.ChangeWorkloadEntryStateToDisconnected(entryName, entryNs, disconTime, origConTime)
}

// GetCleanupGracePeriod implements WorkloadEntryCleaner.
func (c *Controller) GetCleanupGracePeriod() time.Duration {
	return features.WorkloadEntryCleanupGracePeriod
}

// ShouldCleanup implements WorkloadEntryCleaner.
func (c *Controller) ShouldCleanup(wle *config.Config) bool {
	return c.IsHealthCheckedWorkloadEntry(wle) && health.HasHealthCondition(wle) &&
		c.cb.IsExpired(wle, c.GetCleanupGracePeriod())
}

// IsHealthCheckedWorkloadEntry returns true if a given WorkloadEntry represents
// a health-checked workload that is not using auto-registration.
func (c *Controller) IsHealthCheckedWorkloadEntry(wle *config.Config) bool {
	return wle != nil && c.cb.IsControlled(wle) && !autoregistration.IsAutoRegisteredWorkloadEntry(wle)
}

// Cleanup implements WorkloadEntryCleaner.
//
// Updates WorkloadEntry to exclude health status information since we can no longer be certain about it.
func (c *Controller) Cleanup(wle *config.Config, periodic bool) {
	if !c.IsHealthCheckedWorkloadEntry(wle) {
		return
	}
	err := c.stateStore.DeleteHealthCondition(*wle)
	if err != nil {
		log.Warnf("failed cleaning up health-checked WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
		return
	}
	log.Infof("cleaned up health-checked WorkloadEntry %s/%s periodic:%v", wle.Namespace, wle.Name, periodic)
}
