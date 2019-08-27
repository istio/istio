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
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
)

// Controller keeps track of status information for a given K8s style collection and continuously reconciles.
type Controller struct {
	mu sync.Mutex

	states map[resourcekey]*resourcestate

	queue chan resourcekey

	stopCh    chan struct{}
	p         *rt.Provider
	resources []schema.KubeResource
}

type resourcestate struct {
	mu sync.Mutex

	lastKnownVersion resource.Version
	lastKnownStatus  interface{}

	desiredStatus        interface{}
	desiredStatusVersion resource.Version
}

type resourcekey struct {
	collection collection.Name
	resource   resource.Name
}

func (r *resourcestate) setLastKnown(v resource.Version, status interface{}) bool {
	r.mu.Lock()
	r.lastKnownVersion = v
	r.lastKnownStatus = status

	result := r.lastKnownStatus != r.desiredStatus

	r.mu.Unlock()
	return result
}

func (r *resourcestate) setDesired(v resource.Version, status interface{}) bool {
	r.mu.Lock()
	r.desiredStatus = status
	r.desiredStatusVersion = v

	result := r.lastKnownStatus != r.desiredStatus

	r.mu.Unlock()
	return result
}

func (r *resourcestate) needsChange() bool {
	r.mu.Lock()
	result := r.lastKnownStatus != r.desiredStatus
	r.mu.Unlock()
	return result
}

// NewController returns a new instance of controller.
func NewController() *Controller {
	return &Controller{
		queue: make(chan resourcekey, 4096),
	}
}

// UpdateResourceStatus indicates that the target namespace/name has existing status.
func (c *Controller) UpdateResourceStatus(col collection.Name, name resource.Name, version resource.Version, status interface{}) {
	k := resourcekey{collection: col, resource: name}
	if status == nil {
		s := c.tryGetState(k)
		if s != nil {
			if s.setLastKnown(version, status) {
				c.queue <- k
			}
		}
	} else {
		s := c.getState(k)
		if s.setLastKnown(version, status) {
			c.queue <- k
		}
	}
}

// Report the given set of messages towards this particular
func (c *Controller) Report(messages map[collection.Name]map[resource.Name]diag.Messages) {
	c.mu.Lock()
	for key, state := range c.states {
		byResource, ok := messages[key.collection]
		if !ok {
			if state.setDesired(resource.Version(""), nil) {
				c.queue <- key
			}
			continue
		}

		m, ok := byResource[key.resource]
		if !ok || len(m) == 0 {
			if state.setDesired(resource.Version(""), nil) {
				c.queue <- key
			}
			continue
		}

		state.setDesired(m[0].Origin.(*rt.Origin).Version, m)
	}
	c.mu.Unlock()

	for col, byResource := range messages {
		for res, msgs := range byResource {

			k := resourcekey{collection: col, resource: res}
			s := c.getState(k)
			if s.setDesired(msgs[0].Origin.(*rt.Origin).Version, toStatusValue(msgs)) {
				c.queue <- k
			}
		}
	}
}

func (c *Controller) getState(k resourcekey) *resourcestate {
	c.mu.Lock()
	s, ok := c.states[k]
	if !ok {
		s = &resourcestate{}
		c.states[k] = s
	}
	c.mu.Unlock()
	return s
}

func (c *Controller) tryGetState(k resourcekey) *resourcestate {
	c.mu.Lock()
	s := c.states[k]
	c.mu.Unlock()
	return s
}

// Start the controller. This will reset the internal state.
func (c *Controller) Start(p *rt.Provider, resources []schema.KubeResource) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopCh != nil {
		return
	}
	c.states = make(map[resourcekey]*resourcestate)
	c.stopCh = make(chan struct{})
	c.p = p
	c.resources = resources

	go c.run(c.stopCh)
}

func (c *Controller) run(stopCh chan struct{}) {
mainloop:
	for {
		var key resourcekey
		select {
		case <-stopCh:
			break mainloop

		case key = <-c.queue:
		}

		for _, r := range c.resources { // TODO: Rationalize this
			if r.Collection.Name == key.collection {

				iface, err := c.p.GetDynamicResourceInterface(r)
				if err != nil {
					scope.Source.Errorf("Unable to create Client for CRD: %v: %v", r.CanonicalResourceName(), err)
					continue mainloop
				}
				s := c.tryGetState(key)
				if s == nil || !s.needsChange() {
					continue mainloop
				}

				ns, n := key.resource.InterpretAsNamespaceAndName()
				u, err := iface.Namespace(ns).Get(n, metav1.GetOptions{ResourceVersion: string(s.lastKnownVersion)})
				if err != nil {
					scope.Source.Errorf("Unable to read the resource while trying to update status: %v(%v): %v",
						r.CanonicalResourceName(), key.resource, err)
					continue mainloop
				}
				if s.desiredStatusVersion != resource.Version("") && u.GetResourceVersion() != string(s.desiredStatusVersion) {
					scope.Source.Debugf("Skipping due to version mismatch: %v(%v): %v !=% v",
						r.CanonicalResourceName(), key.resource, u.GetResourceVersion(), s.desiredStatusVersion)
					continue mainloop
				}

				if s.desiredStatus != nil {
					u.Object["status"] = s.desiredStatus
				} else {
					delete(u.Object, "status")
				}

				_, err = iface.Namespace(ns).UpdateStatus(u, metav1.UpdateOptions{})
				if err != nil {
					scope.Source.Errorf("Unable to update Status of Resource %v(%v): %v", r.CanonicalResourceName(), key.resource, err)
					continue mainloop
				}
				continue mainloop
			}

			// TODO: log
		}
	}
}

// Stop the controller
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopCh != nil {
		close(c.stopCh)
		c.stopCh = nil
	}
}

func toStatusValue(msgs diag.Messages) interface{} {
	if len(msgs) == 0 {
		return nil
	}

	var lines strings.Builder

	for _, m := range msgs {
		lines.WriteString(m.StatusString())
		lines.WriteString("\n")
	}

	return lines.String()
}
