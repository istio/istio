//  Copyright 2018 Istio Authors
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

package dependency

import "istio.io/istio/pkg/test/internal"

// Fortio indicates a dependency on Fortio.
var Fortio Dependency = &fortio{}

type fortio struct {
}

var _ Dependency = &fortio{}
var _ internal.Stateful = &fortio{}

func (a *fortio) String() string {
	return ""
}

func (a *fortio) Initialize() (interface{}, error) {
	return nil, nil
}

func (a *fortio) Reset(interface{}) error {
	return nil
}

func (a *fortio) Cleanup(interface{}) {

}
