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

package staticvm

import "istio.io/istio/pkg/test/framework/components/namespace"

var _ namespace.Instance = fakeNamespace("")

// fakeNamespace allows matching echo.Configs against a namespace.Instance
type fakeNamespace string

func (f fakeNamespace) Name() string {
	return string(f)
}

func (f fakeNamespace) SetLabel(key, value string) error {
	panic("cannot interact with fake namespace, should not be exposed outside of staticvm")
}

func (f fakeNamespace) RemoveLabel(key string) error {
	panic("cannot interact with fake namespace, should not be exposed outside of staticvm")
}
