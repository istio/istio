// Copyright 2018 Istio Authors
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

package attribute

import (
	"fmt"
	"sort"

	configpb "istio.io/api/policy/v1beta1"
	"istio.io/pkg/pool"
)

// AttributeDescriptorFinder finds attribute descriptors.
// nolint: golint
type AttributeDescriptorFinder interface {
	// GetAttribute finds attribute descriptor in the vocabulary. returns nil if not found.
	GetAttribute(name string) *configpb.AttributeManifest_AttributeInfo
	// Attributes exposes the internal attribute manifest
	Attributes() map[string]*configpb.AttributeManifest_AttributeInfo
}

// NewFinder returns a new AttributeDescriptorFinder instance, based on the given attributes
func NewFinder(attributes map[string]*configpb.AttributeManifest_AttributeInfo) AttributeDescriptorFinder {
	return finder{
		attributes: attributes,
	}
}

// finder exposes expr.AttributeDescriptorFinder
type finder struct {
	attributes map[string]*configpb.AttributeManifest_AttributeInfo
}

var _ AttributeDescriptorFinder = finder{}

// GetAttribute finds an attribute by name. returns nil if not found.
func (a finder) GetAttribute(name string) *configpb.AttributeManifest_AttributeInfo {
	return a.attributes[name]
}

// Attributes exposes the internal attribute manifest.
func (a finder) Attributes() map[string]*configpb.AttributeManifest_AttributeInfo {
	return a.attributes
}

func (a finder) String() string {
	b := pool.GetBuffer()

	// Sort by attribute names for stable ordering.
	i := 0
	names := make([]string, len(a.attributes))
	for name := range a.attributes {
		names[i] = name
		i++
	}
	sort.Strings(names)

	fmt.Fprintln(b, "Attributes:")
	for _, n := range names {
		fmt.Fprintf(b, "  %s: %s", n, a.attributes[n].ValueType.String())
		fmt.Fprintln(b)
	}

	s := b.String()
	pool.PutBuffer(b)
	return s
}

// NewChainedFinder returns an attribute finder that delegates to a parent finder with some overrides.
func NewChainedFinder(parent AttributeDescriptorFinder, overrides map[string]*configpb.AttributeManifest_AttributeInfo) AttributeDescriptorFinder {
	return &chainedFinder{
		parent:    parent,
		overrides: overrides,
	}
}

type chainedFinder struct {
	parent    AttributeDescriptorFinder
	overrides map[string]*configpb.AttributeManifest_AttributeInfo
}

var _ AttributeDescriptorFinder = &chainedFinder{}

// GetAttribute finds an attribute by name. returns nil if not found.
func (a *chainedFinder) GetAttribute(name string) *configpb.AttributeManifest_AttributeInfo {
	if info, exists := a.overrides[name]; exists {
		return info
	}
	return a.parent.GetAttribute(name)
}

func (a *chainedFinder) Attributes() map[string]*configpb.AttributeManifest_AttributeInfo {
	out := make(map[string]*configpb.AttributeManifest_AttributeInfo)
	for name, attr := range a.parent.Attributes() {
		out[name] = attr
	}
	for name, attr := range a.overrides {
		out[name] = attr
	}
	return out
}
