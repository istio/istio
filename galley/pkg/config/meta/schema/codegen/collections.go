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

const staticCollectionsTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

var (
{{range .Entries}}
	// {{.VarName}} is the name of collection {{.Name}}
	{{.VarName}} = collection.NewName("{{.Name}}")
{{end}}
)

// CollectionNames returns the collection names declared in this package.
func CollectionNames() []collection.Name {
	return []collection.Name {
		{{range .Entries}}{{.VarName}},
		{{end}}
	}
}
`

// StaticCollections generates a Go file for static-importing Proto packages, so that they get registered statically.
func StaticCollections(packageName string, collections []string) (string, error) {
	var entries []entry

	for _, col := range collections {
		entries = append(entries, entry{Name: col, VarName: asColVarName(col)})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Name, entries[j].Name) < 0
	})

	context := struct {
		Entries     []entry
		PackageName string
	}{Entries: entries, PackageName: packageName}

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	return applyTemplate(staticCollectionsTemplate, context)
}

func asColVarName(n string) string {
	n = camelCase(n, "/")
	n = camelCase(n, ".")
	return n
}
