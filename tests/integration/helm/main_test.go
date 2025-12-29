//go:build integ

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

package helm

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/resource"
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(adoptCRDsFromPreviousTests).
		Run()
}

// TODO BML this relabeling/reannotating is only required if the previous release is =< 1.23,
// and should be dropped once 1.24 is released.
//
// Needed as other tests do not clean up CRDs and in this suite that can create issues
func adoptCRDsFromPreviousTests(ctx resource.Context) error {
	AdoptPre123CRDResourcesIfNeeded()
	return nil
}
