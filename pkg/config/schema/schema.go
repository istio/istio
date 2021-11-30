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

import (
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/strcase"
)

const (
	kubeCollectionPrefix = "k8s/"
)

// Metadata is the top-level container.
type Metadata struct {
	collections     collection.Schemas
	kubeCollections collection.Schemas
}

// AllCollections is all known collections
func (m *Metadata) AllCollections() collection.Schemas { return m.collections }

// KubeCollections is collections for Kubernetes.
func (m *Metadata) KubeCollections() collection.Schemas { return m.kubeCollections }

func (m *Metadata) Equal(o *Metadata) bool {
	return cmp.Equal(m.collections, o.collections) &&
		cmp.Equal(m.kubeCollections, o.kubeCollections)
}

// ParseAndBuild parses the given metadata file and returns the strongly typed schema.
func ParseAndBuild(yamlText string) (*Metadata, error) {
	mast, err := ast.Parse(yamlText)
	if err != nil {
		return nil, err
	}

	return Build(mast)
}

// Build strongly-typed Metadata from parsed AST.
func Build(astm *ast.Metadata) (*Metadata, error) {
	resourceKey := func(group, kind string) string {
		return group + "/" + kind
	}

	resources := make(map[string]resource.Schema)
	for i, ar := range astm.Resources {
		if ar.Kind == "" {
			return nil, fmt.Errorf("resource %d missing type", i)
		}
		if ar.Plural == "" {
			return nil, fmt.Errorf("resource %d missing plural", i)
		}
		if ar.Version == "" {
			return nil, fmt.Errorf("resource %d missing version", i)
		}
		if ar.Proto == "" {
			return nil, fmt.Errorf("resource %d missing proto", i)
		}
		if ar.ProtoPackage == "" {
			return nil, fmt.Errorf("resource %d missing protoPackage", i)
		}

		if ar.Validate == "" {
			validateFn := "Validate" + strcase.CamelCase(ar.Kind)
			if !validation.IsValidateFunc(validateFn) {
				validateFn = "EmptyValidate"
			}
			ar.Validate = validateFn
		}

		validateFn := validation.GetValidateFunc(ar.Validate)
		if validateFn == nil {
			return nil, fmt.Errorf("failed locating proto validation function %s", ar.Validate)
		}

		r := resource.Builder{
			ClusterScoped: ar.ClusterScoped,
			Kind:          ar.Kind,
			Plural:        ar.Plural,
			Group:         ar.Group,
			Version:       ar.Version,
			Proto:         ar.Proto,
			ProtoPackage:  ar.ProtoPackage,
			ValidateProto: validateFn,
		}.BuildNoValidate()

		key := resourceKey(ar.Group, ar.Kind)
		if _, ok := resources[key]; ok {
			return nil, fmt.Errorf("found duplicate resource for resource (%s)", key)
		}
		resources[key] = r
	}

	cBuilder := collection.NewSchemasBuilder()
	kubeBuilder := collection.NewSchemasBuilder()
	for _, c := range astm.Collections {
		key := resourceKey(c.Group, c.Kind)
		r, found := resources[key]
		if !found {
			return nil, fmt.Errorf("failed locating resource (%s) for collection %s", key, c.Name)
		}

		s, err := collection.Builder{
			Name:     c.Name,
			Disabled: c.Disabled,
			Resource: r,
		}.Build()
		if err != nil {
			return nil, err
		}

		if err = cBuilder.Add(s); err != nil {
			return nil, err
		}

		if isKubeCollection(s.Name()) {
			if err = kubeBuilder.Add(s); err != nil {
				return nil, err
			}
		}
	}
	collections := cBuilder.Build()
	kubeCollections := kubeBuilder.Build()

	return &Metadata{
		collections:     collections,
		kubeCollections: kubeCollections,
	}, nil
}

func isKubeCollection(n collection.Name) bool {
	return strings.HasPrefix(n.String(), kubeCollectionPrefix)
}
