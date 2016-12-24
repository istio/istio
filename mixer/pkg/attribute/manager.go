// Copyright 2016 Google Inc.
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

// Provides the low-level mechanisms to manipulate mixer attributes.
//
// Attributes are name/value pairs providing metadata about an operation.
// In the Istio system, the proxy supplies a potentially large number of
// attributes to the mixer for every request.
//
// There is an 'attribute protocol' between the proxy and the mixer which
// this package implements.
//
// The general use of this package is that there is a single Manager instance
// created for the mixer as a whole. Each incoming gRPC stream uses the manager
// to create a Tracker which is responsible for implementing the aforementioned
// attribute protocol.
//
// The caller invokes the tracker for every incoming request and pushes into it
// an Attributes proto. Doing so produces an attribute Bag which holds the current
// value of all known attributes.
package attribute

// Manager implements the attribute protocol for the mixer's API
//
// There is typically:
//
// * One Manager per mixer instance
// * One Tracker per incoming gRPC streams to the mixer
// * One Bag per attribute context created on individual gRPC streams
type Manager interface {
	// NewTracker returns a Tracker.
	//
	// This method is thread-safe
	NewTracker() Tracker
}

type manager struct {
	dictionaries dictionaries
}

// NewManager allocates a fresh Manager.
//
// There is typically a single Manager allocated per mixer.
func NewManager() Manager {
	return &manager{}
}

func (am *manager) NewTracker() Tracker {
	return getTracker(&am.dictionaries)
}
