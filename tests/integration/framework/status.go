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

// Status is interface to extend the ability of the framework.
// It includes anything needed to be exposed outside to other components, environment.
// Any item (component, environment can has a Config)
// Actual implement can take this interface with its status field.
// It's recommended to use sync.Mutex to lock data while read/write
type Status interface {
}
