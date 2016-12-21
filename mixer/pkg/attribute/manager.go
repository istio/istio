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

package attribute

// Manager implements the attribute protocol for the mixer's API
//
// There is typically:
//
// * One Manager per mixer instance
// * One Tracker per incoming gRPC streams to the mixer
// * One Context per attribute context created on individual gRPC streams
type Manager interface {
	// NewTracker returns an Tracker.
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
	return newTracker(&am.dictionaries)
}
