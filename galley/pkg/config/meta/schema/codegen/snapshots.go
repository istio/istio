// Copyright 2019 Istio Authors
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
)

const staticSnapshotsTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

var (
{{range .Entries}}
	// {{.VarName}} is the name of snapshot {{.Name}}
	{{.VarName}} = "{{.Name}}"
{{end}}
)

// SnapshotNames returns the snapshot names declared in this package.
func SnapshotNames() []string {
	return []string {
		{{range .Entries}}{{.VarName}},
		{{end}}
	}
}
`

// StaticSnapshots generates a Go file for static-declaring Snapshot names.
func StaticSnapshots(packageName string, snapshots []string) (string, error) {
	var entries []entry

	for _, s := range snapshots {
		entries = append(entries, entry{Name: s, VarName: asSnapshotVarName(s)})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Name, entries[j].Name) < 0
	})

	context := struct {
		Entries     []entry
		PackageName string
	}{Entries: entries, PackageName: packageName}

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	return applyTemplate(staticSnapshotsTemplate, context)
}

func asSnapshotVarName(n string) string {
	return CamelCase(n)
}
