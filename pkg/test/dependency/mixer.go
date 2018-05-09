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

import (
	"fmt"
	"reflect"

	"istio.io/istio/pkg/test/cluster"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/internal"
	"istio.io/istio/pkg/test/local"
)

// Mixer indicates a dependency on Mixer.
var Mixer Dependency = &mixer{}

type mixer struct {
	cluster cluster.Internal
	local   local.Internal
}

var _ Dependency = &mixer{}
var _ internal.Stateful = &mixer{}

func (a *mixer) String() string {
	return "mixer"
}

func (a *mixer) Initialize(env environment.Interface) (interface{}, error) {
	if c, ok := env.(cluster.Internal); ok {
		return a.initializeCluster(c)
	} else if l, ok := env.(local.Internal); ok {
		return a.initializeLocal(l)
	} else {
		return nil, fmt.Errorf("unknown environment type: %v", reflect.TypeOf(env))
	}
}

func (a *mixer) Reset(env environment.Interface, state interface{}) error {
	return nil
}

func (a *mixer) Cleanup(env environment.Interface, state interface{}) {

}

func (a *mixer) initializeCluster(e cluster.Internal) (interface{}, error) {
	return nil, nil
}

func (a *mixer) initializeLocal(e local.Internal) (interface{}, error) {
	return nil, nil
}
