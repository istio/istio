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

package echo

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// FactoryFunc can be used by a builder to produce instances from configs
type FactoryFunc func(ctx resource.Context, config []Config) (Instances, error)

var factoryRegistry = map[cluster.Kind]FactoryFunc{}

// RegisterFactory globally registers a base factory of a given Kind.
// The given factory should be immutable, as it will be used globally.
func RegisterFactory(kind cluster.Kind, factory FactoryFunc) {
	factoryRegistry[kind] = factory
}

func GetBuilder(kind cluster.Kind) (FactoryFunc, error) {
	f, ok := factoryRegistry[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported cluster kind: %q", kind)
	}
	return f, nil
}
