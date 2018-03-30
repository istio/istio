//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package crd

import (
	"bytes"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Mapping maps a set of source APIGroup/Version pairs to destination APIGroup/Version pairs.
type Mapping struct {
	forward map[string]string
	reverse map[string]string

	versions map[string]string
}

// NewMapping returns a new mapping based on the provided input GroupVersion map.
func NewMapping(forward map[schema.GroupVersion]schema.GroupVersion) (Mapping, error) {
	f := make(map[string]string, len(forward))
	r := make(map[string]string, len(forward))
	vr := make(map[string]string)

	for k, v := range forward {
		if k.Group == v.Group {
			return Mapping{}, fmt.Errorf("cycling mapping not allowed: %v", k.Group)
		}

		var found bool
		if _, found = f[k.Group]; found {
			return Mapping{}, fmt.Errorf("mapping already exists: %s", k.Group)
		}
		if _, found = r[v.Group]; found {
			return Mapping{}, fmt.Errorf("reverse mapping is not unique: %s", v.Group)
		}

		f[k.Group] = v.Group
		r[v.Group] = k.Group
		vr[v.Group] = v.Version
		vr[k.Group] = k.Version
	}

	return Mapping{
		forward:  f,
		reverse:  r,
		versions: vr,
	}, nil
}

// GetGroupVersion returns the GroupVersion mapping pairs for a given APIGroup.
func (m Mapping) GetGroupVersion(group string) (source, destination schema.GroupVersion, found bool) {

	var sourceGroup string
	var destinationGroup string
	var counterpart string
	if counterpart, found = m.forward[group]; found {
		sourceGroup = group
		destinationGroup = counterpart
		found = true
	} else if counterpart, found = m.reverse[group]; found {
		sourceGroup = counterpart
		destinationGroup = group
		found = true
	}
	source = schema.GroupVersion{
		Group:   sourceGroup,
		Version: m.versions[sourceGroup],
	}

	destination = schema.GroupVersion{
		Group:   destinationGroup,
		Version: m.versions[destinationGroup],
	}

	return
}

// GetVersion Returns the version of an APIGroup.
func (m Mapping) GetVersion(group string) string {
	return m.versions[group]
}

// String output of Mapping.
func (m Mapping) String() string {
	var b bytes.Buffer

	// Alpha sort consistency
	keys := make([]string, 0, len(m.forward))
	for k := range m.forward {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	fmt.Fprint(&b, "--- Mapping ---\n")
	for _, k := range keys {
		rk := m.forward[k]
		fmt.Fprintf(&b, "%s/%s => %s/%s\n", k, m.versions[k], rk, m.versions[rk])
	}
	fmt.Fprint(&b, "---------------\n")

	return b.String()
}
