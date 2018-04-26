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

// RemoteSpyAdapter indicates a dependency on the remote spy adapter.
var RemoteSpyAdapter Dependency = &remoteAdapter{}

type remoteAdapter struct {
}

var _ Dependency = &remoteAdapter{}
var _ internal.Stateful = &remoteAdapter{}

func (r *remoteAdapter) String() string {
	return ""
}

func (r *remoteAdapter) Initialize() (interface{}, error) {
	return nil, nil
}

func (r *remoteAdapter) Reset(interface{}) error {
	return nil
}

func (r *remoteAdapter) Cleanup(interface{}) {

}
