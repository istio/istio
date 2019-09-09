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

	"istio.io/istio/galley/pkg/config/collection"
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
	return fmt.Sprintf("%s/%s", o.Kind, o.Name.String())
}
