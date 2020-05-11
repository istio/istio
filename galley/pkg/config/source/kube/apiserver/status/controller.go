// Copyright 2019 Istio Authors
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

package status

import (
	"context"
	"sync"
	"time"

	"github.com/ghodss/yaml"

	status2 "istio.io/istio/pilot/pkg/status"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

// Controller is the interface for a status controller. It is mainly used to separate implementation from
// interface, so that code can be tested separately.
type Controller interface {
	Start(p *rt.Provider, resources []collection.Schema)
	Stop()
	UpdateResourceStatus(col collection.Name, name resource.FullName, version resource.Version, status interface{})
	Report(messages diag.Messages)
}

// ControllerImpl keeps track of status information for a given K8s style collection and continuously reconciles.
type ControllerImpl struct {
	// Protects the top-level start/stop state of the controller
	mu sync.Mutex

	// Internal state of the controller. It keeps track of known status, desired status, and work queue.
	state *state

	// Wait group for synchronizing the exit of the background go routine.
	wg sync.WaitGroup

	// Subfield of status that this controller manages
	subfield string
}

var _ Controller = &ControllerImpl{}

// NewController returns a new instance of controller.
func NewController(subfield string) *ControllerImpl {
	return &ControllerImpl{
		subfield: subfield,
	}
}

// Start the controller. This will reset the internal state.
func (c *ControllerImpl) Start(p *rt.Provider, resources []collection.Schema) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != nil {
		return
	}
	c.state = newState()

	ifaces := make(map[collection.Name]dynamic.NamespaceableResourceInterface)
	for _, r := range resources {
		if r.IsDisabled() {
			continue
		}

		iface, err := p.GetDynamicResourceInterface(r.Resource())
		if err != nil {
			scope.Source.Errorf("Unable to create a dynamic resource interface for resource %v", r.Resource().GroupVersionKind())
		}
		ifaces[r.Name()] = iface
	}

	c.wg.Add(1)
	go run(c.state, c.subfield, ifaces, &c.wg)
}

// Stop the controller
func (c *ControllerImpl) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != nil {
		c.state.quiesceWork()
		c.wg.Wait()
		c.state = nil
	}
}

// UpdateResourceStatus is called by the source to relay the currently observed status of a resource.
func (c *ControllerImpl) UpdateResourceStatus(
	col collection.Name, name resource.FullName, version resource.Version, status interface{}) {

	// Extract the subfield this controller manages
	// If the status field was something other than a map, treat it like it was an empty map
	// for the purpose of "observed"
	statusMap, _ := status.(map[string]interface{})

	c.state.setObserved(col, name, version, statusMap[c.subfield])
}

// Report the given set of messages towards particular resources.
func (c *ControllerImpl) Report(messages diag.Messages) {
	// TODO: Translating messages in this fashion is expensive, especially on a hot path. We should look for ways
	// to perform this mapping early on, possibly by directly filling up a MessageSet at the analysis context level.
	msgs := NewMessageSet()

	for _, m := range messages {

		if m.Resource == nil {
			// This should not happen. All messages should be reported against at least one resource.
			scope.Source.Errorf("Encountered a diagnostic message without a resource: %v", m)
			continue
		}

		if m.Resource.Origin == nil {
			// This should not happen. All messages should be reported against at least one origin.
			scope.Source.Errorf("Encountered a diagnostic message without an origin: %v", m)
			continue
		}

		origin, ok := m.Resource.Origin.(*rt.Origin)
		if !ok {
			// This should not happen. All messages should be routed back to the appropriate source.
			scope.Source.Errorf("Encountered a diagnostic message with unrecognized origin: %v", m)
			continue
		}

		msgs.Add(origin, m)
	}

	c.state.applyMessages(msgs)
}

func run(state *state, subfield string, ifaces map[collection.Name]dynamic.NamespaceableResourceInterface, wg *sync.WaitGroup) {
mainloop:
	for {
		st, ok := state.dequeueWork()
		if !ok {
			break mainloop
		}

		iface := ifaces[st.key.col]
		if iface == nil {
			scope.Source.Errorf("No updater available for diagnostic message(s) for '%v/%v'.", st.key.col, st.key.res)
			continue
		}

		ns := string(st.key.res.Namespace)
		n := string(st.key.res.Name)
		u, err := iface.Namespace(ns).Get(context.TODO(), n, metav1.GetOptions{ResourceVersion: string(st.observedVersion)})
		if err != nil {
			scope.Source.Errorf("Unable to read the resource while trying to update status: %v(%v): %v",
				st.key.col, st.key.res, err)
			continue mainloop
		}

		// Ensure that the resource we read has the same version as the version for which diagnostic message was
		// generated for.
		if st.desiredStatusVersion != resource.Version("") && u.GetResourceVersion() != string(st.desiredStatusVersion) {
			scope.Source.Debugf("Skipping due to version mismatch: %v(%v): %v !=% v",
				st.key.col, st.key.res, u.GetResourceVersion(), st.desiredStatusVersion)
			continue mainloop
		}

		// Get the map of status objects. If it doesn't already exist, create it.
		statusObj, ok := u.Object["status"]
		if !ok {
			statusObj = make(map[string]interface{})
		}
		statusMap, ok := statusObj.(map[string]interface{})
		if !ok {
			scope.Source.Warnf("Failed to parse the status field as a map. Previous status value will be discarded! Status value was: %v", statusObj)
			statusMap = make(map[string]interface{})
		}

		// Update the status field (for the subfield this controller manages) to match desired status
		// If there are no other subfields left, also delete the status field
		if st.desiredStatus != nil {
			statusMap[subfield] = st.desiredStatus
			statusMap = updateAnalysisCondition(statusMap, true)
			u.Object["status"] = statusMap
		} else {
			delete(statusMap, subfield)
			statusMap = updateAnalysisCondition(statusMap, false)
			if len(statusMap) == 0 {
				delete(u.Object, "status")
			}
		}

		_, err = iface.Namespace(ns).UpdateStatus(context.TODO(), u, metav1.UpdateOptions{})
		if err != nil {
			// TODO: Reinsert work? It probably makes sense to reinsert (with a delay), in case of a transient failure.
			scope.Source.Errorf("Unable to update status of Resource %v(%v): %v", st.key.col, st.key.res, err)
		}
	}
	wg.Done()
}

func getTypedCondition(in interface{}) (out status2.IstioCondition, err error) {
	var statusBytes []byte
	if statusBytes, err = yaml.Marshal(in); err == nil {
		err = yaml.Unmarshal(statusBytes, &out)
	}
	return
}

func removeAnalysisCondition(statusMap map[string]interface{}) map[string]interface{} {
	if statusMap["conditions"] == nil {
		return statusMap
	}
	uconds := statusMap["conditions"].([]interface{})
	for i, ucond := range uconds {
		if cond, err := getTypedCondition(ucond); err == nil && cond.Type == status2.PassedValidation {
			uconds = append(uconds[:i], uconds[i+1:]...)
			break
		}
	}
	statusMap["conditions"] = uconds
	return statusMap
}

func updateAnalysisCondition(statusMap map[string]interface{}, status bool) map[string]interface{} {
	statusMap = removeAnalysisCondition(statusMap)
	if statusMap["conditions"] == nil {
		statusMap["conditions"] = []interface{}{}
	}
	uconds := statusMap["conditions"].([]interface{})
	var cstatus metav1.ConditionStatus
	var message, reason string
	if status {
		cstatus = metav1.ConditionTrue
		reason = "errorsFound"
		message = "Errors Found.  See validationMessages field for more details"
	} else {
		cstatus = metav1.ConditionFalse
		reason = "noErrorsFound"
		message = "No errors Found."
	}
	//uconds = append(uconds, status2.IstioCondition{
	// due to implementation details in the kubernetes fake client
	// we can't used typed objects here at all, as they cannot be deepcopied.
	// typed objects work fine in production, ironically, as deepcopy isn't used.
	uconds = append(uconds, map[string]interface{}{
		"type":               string(status2.PassedValidation),
		"status":             string(cstatus),
		"lastProbeTime":      time.Now().Format(time.RFC3339),
		"lastTransitionTime": time.Now().Format(time.RFC3339),
		"reason":             reason,
		"message":            message,
	})
	statusMap["conditions"] = uconds
	return statusMap
}
