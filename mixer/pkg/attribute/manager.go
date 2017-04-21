// Copyright 2016 Istio Authors
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

// Package attribute provides the low-level mechanisms to manipulate mixer attributes.
//
// Attributes are name/value pairs providing metadata about an operation. In the Istio
// system, Envoy supplies a potentially large number of attributes to Mixer for every
// request and Mixer sends a modest number of attributes back to the proxy with its responses.
//
// There is an 'attribute protocol' between Envoy and Mixer which this package implements.
//
// The general use of this package is that there is a single Manager instance
// created for Mixer as a whole. Each incoming gRPC stream uses the manager
// to create a pair of Trackers which are each responsible for implementing
// 1/2 of the aforementioned attribute protocol (one for the input side, one
// for the output side)
package attribute

// Manager provides support for attributes in Mixer.
type Manager struct {
	dictionaries dictionaries
}

// NewManager allocates a fresh Manager.
func NewManager() *Manager {
	return &Manager{}
}

// NewTracker returns a Tracker.
//
// This method is thread-safe
func (am *Manager) NewTracker() Tracker {
	return getTracker(&am.dictionaries)
}
