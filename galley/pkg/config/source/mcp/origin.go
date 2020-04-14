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

package mcp

import (
	"istio.io/istio/pkg/config/resource"
)

const (
	defaultOrigin    = origin("mcp")
	defaultReference = reference("mcp")
)

var _ resource.Origin = defaultOrigin
var _ resource.Reference = defaultReference

type origin string

func (o origin) FriendlyName() string {
	return string(o)
}

func (o origin) Namespace() resource.Namespace {
	return ""
}

func (o origin) Reference() resource.Reference {
	return defaultReference
}

type reference string

func (r reference) String() string {
	return string(r)
}
