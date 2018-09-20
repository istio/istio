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

package citadel

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
	"log"
)

func TestCitadel(t *testing.T) {
	framework.Requires(t, dependency.Citadel)
}

func TestCitadelKubernetes(t *testing.T) {
	framework.Requires(t, dependency.Kubernetes, dependency.Citadel)
	env := framework.AcquireEnvironment(t)
	c := env.GetCitadelOrFail(t)
	log.Printf("c.Name(): %v", c.CitadelName())
}

// Capturing TestMain allows us to:
// - Do cleanup before exit
// - process testing specific flags
func TestMain(m *testing.M) {
	framework.Run("citadel_test", m)
}
