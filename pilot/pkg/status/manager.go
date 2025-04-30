/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package status

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

// Manager allows multiple controllers to provide input into configuration
// status without needlessly doubling the number of writes, or overwriting
// one another.  Each status controller calls newController, passing in
// an arbitrary status modification function, and then calls EnqueueStatusUpdate
// when an individual resource is ready to be updated with the relevant data.
type Manager struct {
	// TODO: is Resource the right abstraction?
	store   model.ConfigStore
	workers WorkerQueue
}

func NewManager(store model.ConfigStore) *Manager {
	writeFunc := func(m *config.Config) {
		scope.Debugf("writing status for resource %s/%s", m.Namespace, m.Name)
		_, err := store.UpdateStatus(*m)
		if err != nil {
			// TODO: need better error handling
			scope.Errorf("Encountered unexpected error updating status for %v, will try again later: %s", m, err)
			return
		}
	}
	retrieveFunc := func(resource Resource) *config.Config {
		scope.Debugf("retrieving config for status update: %s/%s", resource.Namespace, resource.Name)
		k, ok := gvk.FromGVR(resource.GroupVersionResource)
		if !ok {
			scope.Warnf("GVR %v could not be identified", resource.GroupVersionResource)
			return nil
		}

		current := store.Get(k, resource.Name, resource.Namespace)
		return current
	}
	return &Manager{
		store:   store,
		workers: NewWorkerPool(writeFunc, retrieveFunc, uint(features.StatusMaxWorkers)),
	}
}

func (m *Manager) Start(stop <-chan struct{}) {
	scope.Info("Starting status manager")

	ctx := NewIstioContext(stop)
	m.workers.Run(ctx)
}

// CreateGenericController provides an interface for a status update function to be
// called in series with other controllers, minimizing the number of actual
// api server writes sent from various status controllers.  The UpdateFunc
// must take the target resource status and arbitrary context information as
// parameters, and return the updated status value.  Multiple controllers
// will be called in series, so the input status may not have been written
// to the API server yet, and the output status may be modified by other
// controllers before it is written to the server.
func (m *Manager) CreateGenericController(fn UpdateFunc) *Controller {
	result := &Controller{
		fn:      fn,
		workers: m.workers,
	}
	return result
}

func (m *Manager) CreateIstioStatusController(fn func(status Manipulator, context any)) *Controller {
	result := &Controller{
		fn:      fn,
		workers: m.workers,
	}
	return result
}

// UpdateFunc is called on each object before it is written to allow mutating any status
type UpdateFunc func(status Manipulator, context any)

type Queue interface {
	EnqueueStatusUpdateResource(context any, target Resource)
}

type Controller struct {
	fn      UpdateFunc
	workers WorkerQueue
}

// EnqueueStatusUpdateResource informs the manager that this controller would like to
// update the status of target, using the information in context.  Once the status
// workers are ready to perform this update, the controller's UpdateFunc
// will be called with target and context as input.
func (c *Controller) EnqueueStatusUpdateResource(context any, target Resource) {
	// TODO: buffer this with channel
	c.workers.Push(target, c, context)
}

func (c *Controller) Delete(r Resource) {
	c.workers.Delete(r)
}
