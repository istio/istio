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

package codegen

import (
	"sort"

	"istio.io/istio/pkg/config/schema/ast"
)

const importInitTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	// Pull in all the known proto types to ensure we get their types registered.

{{range .Packages}}
	// Register protos in "{{.}}"
	_ "{{.}}"
{{end}}
)
`

// StaticInit generates a Go file for static-importing Proto packages, so that they get registered statically.
func StaticInit(packageName string, m *ast.Metadata) (string, error) {
	// Single instance and sort names
	names := make(map[string]struct{})

	for _, r := range m.Resources {
		if r.ProtoPackage != "" {
			names[r.ProtoPackage] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(names))
	for p := range names {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	context := struct {
		Packages    []string
		PackageName string
	}{Packages: sorted, PackageName: packageName}

	return applyTemplate(importInitTemplate, context)
}
