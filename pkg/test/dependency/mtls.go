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

// MTLS indicates a dependency on MTLS being enabled.
var MTLS Dependency = &mtls{}

type mtls struct {
}

var _ Dependency = &mtls{}
var _ internal.Stateful = &mtls{}

func (r *mtls) String() string {
	return ""
}

func (r *mtls) Initialize() (interface{}, error) {
	return nil, nil
}

func (r *mtls) Reset(interface{}) error {
	return nil
}

func (r *mtls) Cleanup(interface{}) {

}
