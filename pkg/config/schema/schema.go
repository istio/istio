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
	"sort"
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
	collections       collection.Schemas
	kubeCollections   collection.Schemas
	snapshots         map[string]*Snapshot
	transformSettings []TransformSettings
}

// AllCollections is all known collections
func (m *Metadata) AllCollections() collection.Schemas { return m.collections }

// KubeCollections is collections for Kubernetes.
func (m *Metadata) KubeCollections() collection.Schemas { return m.kubeCollections }

// AllSnapshots returns all known snapshots
func (m *Metadata) AllSnapshots() []*Snapshot {
	result := make([]*Snapshot, 0, len(m.snapshots))
	for _, s := range m.snapshots {
		result = append(result, s)
	}
	return result
}

// TransformSettings is all known transformSettings
func (m *Metadata) TransformSettings() []TransformSettings {
	result := make([]TransformSettings, len(m.transformSettings))
	copy(result, m.transformSettings)
	return result
}

// DirectTransformSettings is a temporary convenience function for getting the Direct TransformSettings config. As the
// infrastructure is generified, then this method should disappear.
func (m *Metadata) DirectTransformSettings() *DirectTransformSettings {
	for _, s := range m.transformSettings {
		if ks, ok := s.(*DirectTransformSettings); ok {
			return ks
		}
	}

	panic("Metadata.DirectTransformSettings: DirectTransformSettings not found")
}

// AllCollectionsInSnapshots returns an aggregate list of names of collections that will appear in the specified snapshots.
func (m *Metadata) AllCollectionsInSnapshots(snapshotNames []string) []string {
	names := make(map[collection.Name]struct{})

	for _, n := range snapshotNames {
		s, ok := m.snapshots[n]
		if !ok {
			panic(fmt.Sprintf("Invalid snapshot name provided: %q", n))
		}
		for _, c := range s.Collections {
			names[c] = struct{}{}
		}
	}

	var result = make([]string, 0, len(names))
	for name := range names {
		result = append(result, name.String())
	}

	sort.SliceStable(result, func(i, j int) bool {
		return strings.Compare(result[i], result[j]) < 0
	})

	return result
}

func (m *Metadata) Equal(o *Metadata) bool {
	return cmp.Equal(m.collections, o.collections) &&
		cmp.Equal(m.kubeCollections, o.kubeCollections) &&
		cmp.Equal(m.snapshots, o.snapshots) &&
		cmp.Equal(m.transformSettings, o.transformSettings)
}

// Snapshot metadata. Describes the snapshots that should be produced.
type Snapshot struct {
	Name        string
	Collections []collection.Name
	Strategy    string
}

// TransformSettings is configuration that is supplied to a particular transform.
type TransformSettings interface {
	Type() string
}

// DirectTransformSettings configuration
type DirectTransformSettings struct {
	mapping map[collection.Name]collection.Name
}

var _ TransformSettings = &DirectTransformSettings{}

// Type implements TransformSettings
func (d *DirectTransformSettings) Type() string {
	return ast.Direct
}

// Mapping from source to destination
func (d *DirectTransformSettings) Mapping() map[collection.Name]collection.Name {
	m := make(map[collection.Name]collection.Name)
	for k, v := range d.mapping {
		m[k] = v
	}

	return m
}

func (d *DirectTransformSettings) Equal(o *DirectTransformSettings) bool {
	return cmp.Equal(d.mapping, o.mapping)
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

		r, err := resource.Builder{
			ClusterScoped: ar.ClusterScoped,
			Kind:          ar.Kind,
			Plural:        ar.Plural,
			Group:         ar.Group,
			Version:       ar.Version,
			Proto:         ar.Proto,
			ProtoPackage:  ar.ProtoPackage,
			ValidateProto: validateFn,
		}.Build()
		if err != nil {
			return nil, err
		}

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

	snapshots := make(map[string]*Snapshot)
	for _, s := range astm.Snapshots {
		sn := &Snapshot{
			Name:     s.Name,
			Strategy: s.Strategy,
		}

		for _, c := range s.Collections {
			col, found := collections.Find(c)
			if !found {
				return nil, fmt.Errorf("collection not found: %v", c)
			}
			sn.Collections = append(sn.Collections, col.Name())
		}
		snapshots[sn.Name] = sn
	}

	var transforms []TransformSettings
	for _, t := range astm.TransformSettings {
		switch v := t.(type) {
		case *ast.DirectTransformSettings:
			mapping := make(map[collection.Name]collection.Name)
			for k, val := range v.Mapping {
				from, ok := collections.Find(k)
				if !ok {
					return nil, fmt.Errorf("collection not found: %v", k)
				}
				to, ok := collections.Find(val)
				if !ok {
					return nil, fmt.Errorf("collection not found: %v", v)
				}
				mapping[from.Name()] = to.Name()
			}
			tr := &DirectTransformSettings{
				mapping: mapping,
			}
			transforms = append(transforms, tr)

		default:
			return nil, fmt.Errorf("unrecognized transform type: %T", t)
		}
	}

	return &Metadata{
		collections:       collections,
		kubeCollections:   kubeCollections,
		snapshots:         snapshots,
		transformSettings: transforms,
	}, nil
}

func isKubeCollection(n collection.Name) bool {
	return strings.HasPrefix(n.String(), kubeCollectionPrefix)
}
