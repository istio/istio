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

package schema

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/labels"
)

// Set defines a set of schema Instances.
type Set []Instance

// Types lists all known types in the schema set
func (s Set) Types() []string {
	types := make([]string, 0, len(s))
	for _, t := range s {
		types = append(types, t.Type)
	}
	return types
}

// GetByType finds a schema by type if it is available
func (s Set) GetByType(name string) (Instance, bool) {
	for _, i := range s {
		if i.Type == name {
			return i, true
		}
	}
	return Instance{}, false
}

// Validate checks that each name conforms to the spec and has a schema Instance
func (s Set) Validate() error {
	var errs error
	descriptorTypes := make(map[string]bool)
	messages := make(map[string]bool)
	clusterMessages := make(map[string]bool)

	for _, v := range s {
		if !labels.IsDNS1123Label(v.Type) {
			errs = multierror.Append(errs, fmt.Errorf("invalid type: %q", v.Type))
		}
		if !labels.IsDNS1123Label(v.Plural) {
			errs = multierror.Append(errs, fmt.Errorf("invalid plural: %q", v.Type))
		}
		if proto.MessageType(v.MessageName) == nil {
			errs = multierror.Append(errs, fmt.Errorf("cannot discover proto message type: %q", v.MessageName))
		}
		if _, exists := descriptorTypes[v.Type]; exists {
			errs = multierror.Append(errs, fmt.Errorf("duplicate type: %q", v.Type))
		}
		descriptorTypes[v.Type] = true
		if v.ClusterScoped {
			if _, exists := clusterMessages[v.MessageName]; exists {
				errs = multierror.Append(errs, fmt.Errorf("duplicate message type: %q", v.MessageName))
			}
			clusterMessages[v.MessageName] = true
		} else {
			if _, exists := messages[v.MessageName]; exists {
				errs = multierror.Append(errs, fmt.Errorf("duplicate message type: %q", v.MessageName))
			}
			messages[v.MessageName] = true
		}
	}
	return errs
}
