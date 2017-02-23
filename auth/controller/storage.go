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

package controller

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

// SecureNamingMapping is a thread-safe storage that maps a service name to a
// set of service accounts that are eligible to run the service.
type SecureNamingMapping struct {
	// Maps from a service name to a set of service accounts that run pods for the service.
	mapping map[string]sets.String

	cond *sync.Cond
}

// NewSecureNamingMapping returns a pointer to a new instance of SecureNamingMapping.
func NewSecureNamingMapping() *SecureNamingMapping {
	return &SecureNamingMapping{
		mapping: map[string]sets.String{},
		cond:    sync.NewCond(&sync.Mutex{}),
	}
}

// AddService adds a service into the mapping. This method does nothing if the
// service already exists.
func (s SecureNamingMapping) AddService(svc string) {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()

	if _, ok := s.mapping[svc]; !ok {
		s.mapping[svc] = sets.NewString()
	}
}

// RemoveService removes a service from the mapping.
func (s SecureNamingMapping) RemoveService(svc string) {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()

	delete(s.mapping, svc)
}

// SetServiceAccounts sets the service accounts for the given service.
func (s SecureNamingMapping) SetServiceAccounts(svc string, accts sets.String) {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()

	s.mapping[svc] = sets.StringKeySet(accts)
}
