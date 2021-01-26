//  Copyright Istio Authors
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

package cluster

import "fmt"

var factoryRegistry = map[Kind]Factory{}

// RegisterFactory globally registers a base factory of a given Kind.
// The given factory should be immutable, as it will be used globally.
func RegisterFactory(factory Factory) {
	factoryRegistry[factory.Kind()] = factory
}

func GetFactory(kind Kind) (Factory, error) {
	f, ok := factoryRegistry[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported cluster kind: %q", kind)
	}
	return f, nil
}
