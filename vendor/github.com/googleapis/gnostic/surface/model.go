// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package surface_v1

import (
	"path"
	"strings"
)

func (m *Model) addType(t *Type) {
	m.Types = append(m.Types, t)
}

func (m *Model) addMethod(method *Method) {
	m.Methods = append(m.Methods, method)
}

func (m *Model) TypeWithTypeName(name string) *Type {
	if name == "" {
		return nil
	}
	for _, t := range m.Types {
		if t.TypeName == name {
			return t
		}
	}
	return nil
}

func generateOperationName(method, path string) string {
	filteredPath := strings.Replace(path, "/", "_", -1)
	filteredPath = strings.Replace(filteredPath, ".", "_", -1)
	filteredPath = strings.Replace(filteredPath, "{", "", -1)
	filteredPath = strings.Replace(filteredPath, "}", "", -1)
	return strings.Title(method) + filteredPath
}

func sanitizeOperationName(name string) string {
	name = strings.Title(name)
	name = strings.Replace(name, ".", "_", -1)
	return name
}

func typeForRef(ref string) (typeName string) {
	return path.Base(ref)
}
