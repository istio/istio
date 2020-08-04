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
	"strings"

	"istio.io/istio/pkg/config/schema/ast"
)

const staticSnapshotsTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{ .PackageName }}

var (
{{ range .Entries }}
	{{ commentBlock (wordWrap (printf "%s %s" .VariableName .Description) 70) 1 }}
	{{ .VariableName }} = "{{ .Name }}"
{{ end }}
)

// SnapshotNames returns the snapshot names declared in this package.
func SnapshotNames() []string {
	return []string {
		{{ range .Entries }}{{ .VariableName }},
		{{ end }}
	}
}
`

// StaticSnapshots generates a Go file for static-declaring Snapshot names.
func StaticSnapshots(packageName string, m *ast.Metadata) (string, error) {
	sort.Slice(m.Snapshots, func(i, j int) bool {
		return strings.Compare(m.Snapshots[i].Name, m.Snapshots[j].Name) < 0
	})

	context := struct {
		Entries     []*ast.Snapshot
		PackageName string
	}{Entries: m.Snapshots, PackageName: packageName}

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	return applyTemplate(staticSnapshotsTemplate, context)
}
