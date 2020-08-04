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

package k8smeta

// Embed the core metadata file containing the collections as a resource
//go:generate go-bindata --nocompress --nometadata --pkg k8smeta -o k8smeta.gen.go  k8smeta.yaml

// Create static initializers file
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/staticinit.main.go k8smeta k8smeta.yaml staticinit.gen.go

// Create collection constants
//go:generate go run $REPO_ROOT/pkg/config/schema/codegen/tools/collections.main.go k8smeta k8smeta.yaml collections.gen.go

//go:generate goimports -w -local istio.io "$REPO_ROOT/galley/pkg/config/testing/k8smeta/collections.gen.go"
//go:generate goimports -w -local istio.io "$REPO_ROOT/galley/pkg/config/testing/k8smeta/staticinit.gen.go"
