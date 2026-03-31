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

package state

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	istiolog "istio.io/istio/pkg/log"
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

// Store knows how to keep internal state as part of a WorkloadEntry resource.
type Store struct {
	store model.ConfigStoreController
	cb    StoreCallbacks
}

// StoreCallbacks represents a contract between a Store and
// a autoregistration.Controller.
type StoreCallbacks interface {
	// IsControllerOf returns true if a given WorkloadEntry is connected
	// to this istiod instance.
	IsControllerOf(wle *config.Config) bool
}

// NewStore returns a new Store instance.
func NewStore(store model.ConfigStoreController, cb StoreCallbacks) *Store {
	return &Store{
		store: store,
		cb:    cb,
	}
}

// UpdateHealth updates the associated WorkloadEntries health status
// based on the corresponding health check performed by istio-agent.
func (s *Store) UpdateHealth(proxyID, entryName, entryNs string, condition *v1alpha1.IstioCondition) error {
	// get previous status
	cfg := s.store.Get(gvk.WorkloadEntry, entryName, entryNs)
	if cfg == nil {
		return fmt.Errorf("failed to update health status for %v: WorkloadEntry %v not found", proxyID, entryNs)
	}
	// The workloadentry has reconnected to the other istiod
	if !s.cb.IsControllerOf(cfg) {
		return nil
	}

	// check if the existing health status is newer than this one
	wleStatus, ok := cfg.Status.(*v1alpha1.IstioStatus)
	if ok {
		healthCondition := status.GetCondition(wleStatus.Conditions, status.ConditionHealthy)
		if healthCondition != nil {
			if healthCondition.LastProbeTime.AsTime().After(condition.LastProbeTime.AsTime()) {
				return nil
			}
		}
	}

	// replace the updated status
	wle := status.UpdateIstioConfigCondition(*cfg, condition)
	// update the status
	_, err := s.store.UpdateStatus(wle)
	if err != nil {
		return fmt.Errorf("error while updating WorkloadEntry health status for %s: %w", proxyID, err)
	}
	log.Debugf("updated health status of %v to %v", proxyID, condition)
	return nil
}

// DeleteHealthCondition updates WorkloadEntry of a workload that is not using auto-registration
// to remove information about the health status (since we can no longer be certain about it).
func (s *Store) DeleteHealthCondition(wle config.Config) error {
	wle = status.DeleteIstioConfigCondition(wle, status.ConditionHealthy)
	// update the status
	_, err := s.store.UpdateStatus(wle)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error while removing WorkloadEntry health status for %s/%s: %v", wle.Namespace, wle.Name, err)
	}
	return nil
}
