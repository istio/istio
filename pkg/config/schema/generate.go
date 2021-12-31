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

// Create collection constants
// We will generate collections twice. Once is the full collection set. The other includes only Istio types, with build tags set for agent
// This allows the agent to use collections without importing all of Kuberntes libraries
// nolint: lll
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/collections.main.go collections metadata.yaml "$REPO_ROOT/pkg/config/schema/collections/collections.gen.go" k8s "$REPO_ROOT/pkg/config/schema/collections/collections.agent.gen.go" "agent"
// Create gvk helpers
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/collections.main.go gvk metadata.yaml "$REPO_ROOT/pkg/config/schema/gvk/resources.gen.go"

//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/collections/collections.gen.go"
//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/collections/collections.agent.gen.go"
