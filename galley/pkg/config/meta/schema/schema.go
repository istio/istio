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

package schema

import (
	"fmt"
	"sort"
	"strings"

	"istio.io/istio/galley/pkg/config/meta/schema/ast"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

// Metadata is the top-level container.
type Metadata struct {
	collections       collection.Specs
	snapshots         map[string]*Snapshot
	sources           []Source
	transformSettings []TransformSettings
}

// AllCollections is all known collections
func (m *Metadata) AllCollections() collection.Specs { return m.collections }

// AllSnapshots returns all known snapshots
func (m *Metadata) AllSnapshots() []*Snapshot {
	result := make([]*Snapshot, 0, len(m.snapshots))
	for _, s := range m.snapshots {
		result = append(result, s)
	}
	return result
}

// AllSources is all known sources
func (m *Metadata) AllSources() []Source {
	result := make([]Source, len(m.sources))
	copy(result, m.sources)
	return result
}

// KubeSource is a temporary convenience function for getting the Kubernetes Source. As the infrastructure
// is generified, then this method should disappear.
func (m *Metadata) KubeSource() *KubeSource {
	for _, s := range m.sources {
		if ks, ok := s.(*KubeSource); ok {
			return ks
		}
	}

	panic("Metadata.KubeSource: KubeSource not found")
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

// Snapshot metadata. Describes the snapshots that should be produced.
type Snapshot struct {
	Name        string
	Collections []collection.Name
	Strategy    string
}

// Source configuration metadata.
type Source interface {
}

// TransformSettings is configuration that is supplied to a particular transform.
type TransformSettings interface {
	Type() string
}

// KubeSource is configuration for K8s based input sources.
type KubeSource struct {
	resources []*KubeResource
}

// Resources is all known K8s resources
func (k *KubeSource) Resources() KubeResources {
	result := make([]KubeResource, len(k.resources))
	for i, r := range k.resources {
		result[i] = *r
	}
	return result
}

var _ Source = &KubeSource{}

// KubeResource metadata for a Kubernetes KubeResource.
type KubeResource struct {
	Collection    collection.Spec
	Group         string
	Version       string
	Kind          string
	Plural        string
	Disabled      bool
	ClusterScoped bool
}

// KubeResources is an array of resources
type KubeResources []KubeResource

// CanonicalResourceName of the resource.
func (i KubeResource) CanonicalResourceName() string {
	if i.Group == "" {
		return "core/" + i.Version + "/" + i.Kind
	}
	return i.Group + "/" + i.Version + "/" + i.Kind
}

// Collections returns the name of collections for this set of resources
func (k KubeResources) Collections() []collection.Name {
	result := make([]collection.Name, 0, len(k))
	for _, res := range k {
		result = append(result, res.Collection.Name)
	}

	return result
}

// Find searches and returns the resource spec with the given group/kind
func (k KubeResources) Find(group, kind string) (KubeResource, bool) {
	for _, rs := range k {
		if rs.Group == group && rs.Kind == kind {
			return rs, true
		}
	}

	return KubeResource{}, false
}

// MustFind calls Find and panics if not found.
func (k KubeResources) MustFind(group, kind string) KubeResource {
	r, found := k.Find(group, kind)
	if !found {
		panic(fmt.Sprintf("KubeSource.MustFind: unable to find %s/%s", group, kind))
	}
	return r
}

// DisabledCollections returns the names of disabled collections
func (k KubeResources) DisabledCollections() collection.Names {
	disabledCollections := make([]collection.Name, 0)
	for _, r := range k {
		if r.Disabled {
			disabledCollections = append(disabledCollections, r.Collection.Name)
		}
	}
	return disabledCollections
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
	b := collection.NewSpecsBuilder()
	for _, c := range astm.Collections {
		s, err := collection.NewSpec(c.Name, c.ProtoPackage, c.Proto)
		if err != nil {
			return nil, err
		}

		if err = b.Add(s); err != nil {
			return nil, err
		}
	}
	collections := b.Build()

	snapshots := make(map[string]*Snapshot)
	for _, s := range astm.Snapshots {
		sn := &Snapshot{
			Name:     s.Name,
			Strategy: s.Strategy,
		}

		for _, c := range s.Collections {
			col, found := collections.Lookup(c)
			if !found {
				return nil, fmt.Errorf("collection not found: %v", c)
			}
			sn.Collections = append(sn.Collections, col.Name)
		}
		snapshots[sn.Name] = sn
	}

	var sources []Source
	for _, s := range astm.Sources {
		switch v := s.(type) {
		case *ast.KubeSource:
			var resources []*KubeResource
			for i, r := range v.Resources {
				if r == nil {
					return nil, fmt.Errorf("invalid KubeResource entry at position: %d", i)
				}
				col, ok := collections.Lookup(r.Collection)
				if !ok {
					return nil, fmt.Errorf("collection not found: %v", r.Collection)
				}
				res := &KubeResource{
					Collection:    col,
					Kind:          r.Kind,
					Plural:        r.Plural,
					Version:       r.Version,
					Group:         r.Group,
					Disabled:      r.Disabled,
					ClusterScoped: r.ClusterScoped,
				}

				resources = append(resources, res)
			}
			src := &KubeSource{
				resources: resources,
			}
			sources = append(sources, src)

		default:
			return nil, fmt.Errorf("unrecognized source type: %T", s)
		}
	}

	var transforms []TransformSettings
	for _, t := range astm.TransformSettings {
		switch v := t.(type) {
		case *ast.DirectTransformSettings:
			mapping := make(map[collection.Name]collection.Name)
			for k, val := range v.Mapping {
				from, ok := collections.Lookup(k)
				if !ok {
					return nil, fmt.Errorf("collection not found: %v", k)
				}
				to, ok := collections.Lookup(val)
				if !ok {
					return nil, fmt.Errorf("collection not found: %v", v)
				}
				mapping[from.Name] = to.Name
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
		snapshots:         snapshots,
		sources:           sources,
		transformSettings: transforms,
	}, nil
}
