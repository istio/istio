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

package store

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

const (
	// TODO use status or another proper API instead of annotations

	// WorkloadControllerAnnotation on a WorkloadEntry should store the current/last pilot instance connected to the workload for XDS.
	WorkloadControllerAnnotation = "istio.io/workloadController"

	// ConnectedAtAnnotation on a WorkloadEntry stores the time in nanoseconds when the associated workload connected to a Pilot instance.
	ConnectedAtAnnotation = "istio.io/connectedAt"
	// DisconnectedAtAnnotation on a WorkloadEntry stores the time in nanoseconds when the associated workload disconnected from a Pilot instance.
	DisconnectedAtAnnotation = "istio.io/disconnectedAt"

	timeFormat = time.RFC3339Nano
)

// Controller knows how to keep internal state as part of a WorkloadEntry resource.
type Controller struct {
	instanceID string

	store model.ConfigStoreController
}

// NewController return a new Controller instance.
func NewController(store model.ConfigStoreController, instanceID string) *Controller {
	c := &Controller{
		instanceID: instanceID,
		store:      store,
	}
	return c
}

// ChangeStateToConnected updates given WorkloadEntry to reflect that
// it is now connected to this particular `istiod` instance.
func (c *Controller) ChangeStateToConnected(entryName, entryNs string, conTime time.Time) (bool, error) {
	wle := c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
	if wle == nil {
		return false, fmt.Errorf("failed updating WorkloadEntry %s/%s: WorkloadEntry not found", entryNs, entryName)
	}
	lastConTime, _ := time.Parse(timeFormat, wle.Annotations[ConnectedAtAnnotation])
	// the proxy has reconnected to another pilot, not belong to this one.
	if conTime.Before(lastConTime) {
		return false, nil
	}
	// Try to patch, if it fails then try to create
	_, err := c.store.Patch(*wle, func(cfg config.Config) (config.Config, kubetypes.PatchType) {
		setConnectMeta(&cfg, c.instanceID, conTime)
		return cfg, kubetypes.MergePatchType
	})
	if err != nil {
		return false, fmt.Errorf("failed updating WorkloadEntry %s/%s err: %v", entryNs, entryName, err)
	}
	return true, nil
}

// ChangeStateToDisconnected updates given WorkloadEntry to reflect that
// it is no longer connected to this particular `istiod` instance.
func (c *Controller) ChangeStateToDisconnected(entryName, entryNs string, disconTime, origConnTime time.Time) (bool, error) {
	// unset controller, set disconnect time
	cfg := c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
	if cfg == nil {
		log.Infof("workloadentry %s/%s is not found, maybe deleted or because of propagate latency",
			entryNs, entryName)
		// return error and backoff retry to prevent workloadentry leak
		return false, fmt.Errorf("workloadentry %s/%s is not found", entryNs, entryName)
	}

	// only queue a delete if this disconnect event is associated with the last connect event written to the workload entry
	if mostRecentConn, err := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation]); err == nil {
		if mostRecentConn.After(origConnTime) {
			// this disconnect event wasn't processed until after we successfully reconnected
			return false, nil
		}
	}
	// The wle has reconnected to another istiod and controlled by it.
	if cfg.Annotations[WorkloadControllerAnnotation] != c.instanceID {
		return false, nil
	}

	conTime, _ := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation])
	// The wle has reconnected to this istiod,
	// this may happen when the unregister fails retry
	if disconTime.Before(conTime) {
		return false, nil
	}

	wle := cfg.DeepCopy()
	delete(wle.Annotations, ConnectedAtAnnotation)
	wle.Annotations[DisconnectedAtAnnotation] = disconTime.Format(timeFormat)
	// use update instead of patch to prevent race condition
	_, err := c.store.Update(wle)
	if err != nil {
		return false, fmt.Errorf("disconnect: failed updating WorkloadEntry %s/%s: %v", entryNs, entryName, err)
	}
	return true, nil
}

// UpdateHealth updates the associated WorkloadEntries health status
// based on the corresponding health check performed by istio-agent.
func (c *Controller) UpdateHealth(proxyID, entryName, entryNs string, condition *v1alpha1.IstioCondition) error {
	// get previous status
	cfg := c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
	if cfg == nil {
		return fmt.Errorf("failed to update health status for %v: WorkloadEntry %v not found", proxyID, entryNs)
	}
	// The workloadentry has reconnected to the other istiod
	if !IsControlledBy(cfg, c.instanceID) {
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
	wle := status.UpdateConfigCondition(*cfg, condition)
	// update the status
	_, err := c.store.UpdateStatus(wle)
	if err != nil {
		return fmt.Errorf("error while updating WorkloadEntry health status for %s: %v", proxyID, err)
	}
	log.Debugf("updated health status of %v to %v", proxyID, condition)
	return nil
}

// Get retrieves a WorkloadEntry by key.
func (c *Controller) Get(entryName, entryNs string) *config.Config {
	return c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
}

// Create adds a new cWorkloadEntry object to the store.
func (c *Controller) Create(entry config.Config, conTime time.Time) (revision string, err error) {
	setConnectMeta(&entry, c.instanceID, conTime)
	return c.store.Create(entry)
}

// Delete removes WorkloadEntry that was created automatically for a workload
// that is using auto-registration.
func (c *Controller) Delete(wle *config.Config) error {
	err := c.store.Delete(gvk.WorkloadEntry, wle.Name, wle.Namespace, &wle.ResourceVersion)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func setConnectMeta(wle *config.Config, controller string, conTime time.Time) {
	if wle.Annotations == nil {
		wle.Annotations = map[string]string{}
	}
	wle.Annotations[WorkloadControllerAnnotation] = controller
	wle.Annotations[ConnectedAtAnnotation] = conTime.Format(timeFormat)
	delete(wle.Annotations, DisconnectedAtAnnotation)
}
