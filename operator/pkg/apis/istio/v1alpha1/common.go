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

package v1alpha1

import (
	"fmt"
	"reflect"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"istio.io/api/operator/v1alpha1"
)

const (
	globalKey         = "global"
	istioNamespaceKey = "istioNamespace"
)

// Namespace returns the namespace of the containing CR.
func Namespace(iops *v1alpha1.IstioOperatorSpec) string {
	if iops.Namespace != "" {
		return iops.Namespace
	}
	if iops.Values == nil {
		return ""
	}
	v := iops.Values.AsMap()
	if v[globalKey] == nil {
		return ""
	}
	vg := v[globalKey].(map[string]any)
	n := vg[istioNamespaceKey]
	if n == nil {
		return ""
	}
	return n.(string)
}

// SetNamespace returns the namespace of the containing CR.
func SetNamespace(iops *v1alpha1.IstioOperatorSpec, namespace string) {
	if namespace != "" {
		iops.Namespace = namespace
	}
	// TODO implement
}

// GetFieldType uses reflection to retrieve the reflect.Type of a nested field
// As the IstioOperator type only uses a map[string]any for values, it will
// traverse either IstioOperatorSpec or Values depending on the given path
func GetFieldType(path string) (reflect.Type, error) {
	titleCaser := cases.Title(language.English, cases.NoLower)
	iopSpec := v1alpha1.IstioOperatorSpec{}
	values := Values{}
	path = strings.TrimPrefix(path, "spec.")
	pathParts := strings.Split(path, ".")
	var currentNode reflect.Type
	// traverse either values or iopSpec
	currentPath := "spec"
	if len(pathParts) > 0 && pathParts[0] == "values" {
		currentNode = reflect.TypeOf(&values).Elem()
		pathParts = pathParts[1:]
		currentPath += ".values"
	} else {
		currentNode = reflect.TypeOf(&iopSpec).Elem()
	}
	for _, part := range pathParts {
		// remove any slice brackets from the path
		if sliceBrackets := strings.Index(part, "["); sliceBrackets > 0 {
			part = part[:sliceBrackets]
		}
		currentPath += "." + part
		nextNode, found := currentNode.FieldByName(titleCaser.String(part))
		if !found {
			// iterate over fields to compare against json tags
			for i := 0; i < currentNode.NumField(); i++ {
				f := currentNode.Field(i)
				if strings.Contains(f.Tag.Get("protobuf"), "json="+part+",") {
					nextNode = f
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("failed to identify type of %s: field %s does not exist", path, currentPath)
			}
		}
		if nextNode.Type.Kind() == reflect.Slice || nextNode.Type.Kind() == reflect.Pointer {
			currentNode = nextNode.Type.Elem()
		} else if nextNode.Type.Kind() == reflect.Map {
			currentNode = nextNode.Type.Elem()
		} else {
			currentNode = nextNode.Type
		}
		if currentNode.Kind() != reflect.Struct {
			break
		}
	}
	return currentNode, nil
}
