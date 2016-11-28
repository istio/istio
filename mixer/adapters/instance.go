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

package adapters

// InstanceConfig is used to configure an individual instance of an adapter.
type InstanceConfig interface{}

// Instance defines lifecycle functions for individual adapter instances.
type Instance interface {
	// Delete is called by the mixer to indicate it is done with a particular adapter instance.
	Delete()

	// UpdateConfig is used to refresh an adapter instance's live configuration.
	UpdateConfig(config InstanceConfig) error
}
