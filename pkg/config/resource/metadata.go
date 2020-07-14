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

package resource

import (
	"time"

	"istio.io/istio/pkg/config/schema/resource"
)

// Metadata about a resource.
type Metadata struct {
	Schema      resource.Schema
	FullName    FullName
	CreateTime  time.Time
	Version     Version
	Labels      StringMap
	Annotations StringMap
}

// Clone Metadata. Warning, this is expensive!
func (m *Metadata) Clone() Metadata {
	result := *m
	result.Annotations = m.Annotations.Clone()
	result.Labels = m.Labels.Clone()
	return result
}
