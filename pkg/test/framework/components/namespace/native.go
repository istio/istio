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

package namespace

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/resource"

	"github.com/google/uuid"
)

// nativeNamespace represents an imaginary namespace. It is tracked as a resource.
type nativeNamespace struct {
	id   resource.ID
	name string
}

var _ Instance = &nativeNamespace{}
var _ resource.Resource = &nativeNamespace{}

func (n *nativeNamespace) Name() string {
	return n.name
}

func (n *nativeNamespace) ID() resource.ID {
	return n.id
}

func claimNative(_ resource.Context, name string) *nativeNamespace {
	return &nativeNamespace{name: name}
}

func newNative(ctx resource.Context, prefix string, _ bool) *nativeNamespace {
	ns := fmt.Sprintf("%s-%s", prefix, uuid.New().String())

	n := &nativeNamespace{name: ns}
	n.id = ctx.TrackResource(n)

	return n
}
