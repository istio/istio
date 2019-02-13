//  Copyright 2019 Istio Authors
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

package registry

import (
	"fmt"

	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/environment/kube"
	"istio.io/istio/pkg/test/framework2/components/environment/native"
)

// Names of currently registered environments
func Names() []string {
	return []string{
		native.Name,
		kube.Name,
	}
}

// DefaultName is the name of the default environment.
func DefaultName() string {
	return native.Name
}

// New returns a new instance of the named environment.
func New(name string, ctx environment.Context) (environment.Instance, error) {
	switch name {
	case native.Name:
		return native.New(ctx)
	case kube.Name:
		return kube.New(ctx)
	default:
		return nil, fmt.Errorf("unknown environment: %q", name)
	}
}
