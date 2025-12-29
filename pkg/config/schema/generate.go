// Copyright Istio Authors
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

package schema

// Create collection constants.
// We will generate collections twice. Once is the full collection set.
// The other includes only Istio types, with build tags set for agent.
// This allows the agent to use collections without importing all of Kubernetes libraries.
//go:generate go run codegen/tools/collections.main.go
