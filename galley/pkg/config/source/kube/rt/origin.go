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

package rt

import (
	"fmt"
	"path/filepath"
	"strings"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// Origin is a K8s specific implementation of resource.Origin
type Origin struct {
	Collection collection.Name
	Kind       string
	FullName   resource.FullName
	Version    resource.Version
	Ref        resource.Reference
}

var _ resource.Origin = &Origin{}
var _ resource.Reference = &Position{}

// FriendlyName implements resource.Origin
func (o *Origin) FriendlyName() string {
	parts := strings.Split(o.FullName.String(), "/")
	if len(parts) == 2 {
		// The istioctl convention is <type> <name>[.<namespace>].
		// This code has no notion of a default and always shows the namespace.
		return fmt.Sprintf("%s %s.%s", o.Kind, parts[1], parts[0])
	}
	return fmt.Sprintf("%s %s", o.Kind, o.FullName.String())
}

// Namespace implements resource.Origin
func (o *Origin) Namespace() resource.Namespace {
	// Special case: the namespace of a namespace resource is its own name
	if o.Collection == collections.K8SCoreV1Namespaces.Name() {
		return resource.Namespace(o.FullName.Name)
	}

	return o.FullName.Namespace
}

// Reference implements resource.Origin
func (o *Origin) Reference() resource.Reference {
	return o.Ref
}

// Position is a representation of the location of a source.
type Position struct {
	Filename string // filename, if any
	Line     int    // line number, starting at 1
}

// String outputs the string representation of the position.
func (p *Position) String() string {
	s := p.Filename
	// TODO: support json file position.
	if p.isValid() && filepath.Ext(p.Filename) != ".json" {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprintf("%d", p.Line)
	}
	return s
}

func (p *Position) isValid() bool {
	return p.Line > 0 && p.Filename != ""
}
