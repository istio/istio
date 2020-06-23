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

// Embed the core metadata file containing the collections as a resource
//go:generate go-bindata --nocompress --nometadata --pkg schema -o metadata.gen.go metadata.yaml

// Create static initializers files in each of the output directories
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/staticinit.main.go schema metadata.yaml staticinit.gen.go
// nolint: lll
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/staticinit.main.go collections metadata.yaml "$REPO_ROOT/pkg/config/schema/collections/staticinit.gen.go"
// nolint: lll
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/staticinit.main.go snapshots metadata.yaml "$REPO_ROOT/pkg/config/schema/snapshots/staticinit.gen.go"

// Create collection constants
// nolint: lll
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/collections.main.go collections metadata.yaml "$REPO_ROOT/pkg/config/schema/collections/collections.gen.go"
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/collections.main.go gvk metadata.yaml "$REPO_ROOT/pkg/config/schema/gvk/resources.gen.go"

// Create snapshot constants
// nolint: lll
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/snapshots.main.go snapshots metadata.yaml "$REPO_ROOT/pkg/config/schema/snapshots/snapshots.gen.go"

//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/collections/collections.gen.go"
//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/snapshots/snapshots.gen.go"
//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/staticinit.gen.go"
//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/collections/staticinit.gen.go"
//go:generate goimports -w -local istio.io "$REPO_ROOT/pkg/config/schema/snapshots/staticinit.gen.go"
