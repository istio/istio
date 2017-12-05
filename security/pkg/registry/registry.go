// Copyright 2017 Istio Authors
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

package registry

import (
	"sync"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"

	"istio.io/istio/pilot/platform/kube"
)

// Registry is the standard interface for identity registry
// implementation
type Registry interface {
	Check(string, string) bool
	AddMapping(string, string)
	DeleteMapping(string, string)
}

// IdentityRegistry is a naive registry that maintains a mapping between
// identities (as strings)
type IdentityRegistry struct {
	sync.RWMutex
	Map map[string]string
}

// Check checks whether id1 is mapped to id2
func (reg *IdentityRegistry) Check(id1, id2 string) bool {
	reg.RLock()
	mapped, ok := reg.Map[id1]
	reg.RUnlock()
	if !ok {
		return false
	}
	return id2 == mapped
}

// AddMapping adds a mapping id1 -> id2
func (reg *IdentityRegistry) AddMapping(id1, id2 string) {
	reg.RLock()
	oldID, ok := reg.Map[id1]
	reg.RUnlock()
	if ok {
		glog.Warningf("Overwriting existing mapping: %q -> %q", id1, oldID)
	}
	reg.Lock()
	reg.Map[id1] = id2
	reg.Unlock()
}

// DeleteMapping attempts to delete mapping id1 -> id2. If id1 is already
// mapped to a different identity, deletion fails
func (reg *IdentityRegistry) DeleteMapping(id1, id2 string) {
	reg.RLock()
	oldID, ok := reg.Map[id1]
	reg.RUnlock()
	if !ok || oldID != id2 {
		glog.Warningf("Attempting to delete nonexistent mapping: %q -> %q", id1, id2)
		return
	}
	reg.Lock()
	delete(reg.Map, id1)
	reg.Unlock()
}

var (
	// singleton object of identity registry
	reg Registry
)

// GetIdentityRegistry returns the identity registry object
func GetIdentityRegistry() Registry {
	if reg == nil {
		reg = &IdentityRegistry{
			Map: make(map[string]string),
		}
	}
	return reg
}

// K8SServiceAdded ...
func K8SServiceAdded(svc *v1.Service) {
	svcAcct, ok := svc.ObjectMeta.Annotations[kube.KubeServiceAccountsOnVMAnnotation]
	if ok {
		GetIdentityRegistry().AddMapping(svcAcct, svcAcct)
	}
}

// K8SServiceDeleted ...
func K8SServiceDeleted(svc *v1.Service) {
	svcAcct, ok := svc.ObjectMeta.Annotations[kube.KubeServiceAccountsOnVMAnnotation]
	if ok {
		GetIdentityRegistry().DeleteMapping(svcAcct, svcAcct)
	}
}

// K8SServiceUpdated ...
func K8SServiceUpdated(oldSvc, newSvc *v1.Service) {
	K8SServiceDeleted(oldSvc)
	K8SServiceAdded(newSvc)
}
