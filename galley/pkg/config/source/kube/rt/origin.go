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

package rt

import (
	"fmt"
	"strings"

	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// Origin is a K8s specific implementation of resource.Origin
type Origin struct {
	Collection collection.Name
	Kind       string
	Name       resource.Name
	Version    resource.Version
}

var _ resource.Origin = &Origin{}

// FriendlyName implements resource.Origin
func (o *Origin) FriendlyName() string {
	parts := strings.Split(o.Name.String(), "/")
	if len(parts) == 2 {
		// The istioctl convention is <type> <name>[.<namespace>].
		// This code has no notion of a default and always shows the namespace.
		return fmt.Sprintf("%s %s.%s", o.Kind, parts[1], parts[0])
	}
	return fmt.Sprintf("%s %s", o.Kind, o.Name.String())
}

// Namespace implements resource.Origin
func (o *Origin) Namespace() string {
	// Special case: the namespace of a namespace resource is its own name
	if o.Collection == metadata.K8SCoreV1Namespaces {
		return o.Name.String()
	}

	ns, _ := o.Name.InterpretAsNamespaceAndName()
	return ns
}
