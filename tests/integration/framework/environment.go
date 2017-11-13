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

package framework

// TestEnv is a interface holding serveral components for testing
type TestEnv interface {

	// GetName return environment ID
	GetName() string

	// GetComponents is the key of a environment
	// It defines what components a environment contains.
	GetComponents() []Component

	// Bringup doing general setup for environment level, not components.
	Bringup() error

	// Cleanup clean everything created by this test environment, not component level
	Cleanup() error
}
