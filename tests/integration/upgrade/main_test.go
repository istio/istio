// +build integ
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

package upgrade

import (
	"log"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	Latest    istio.Instance
	NMinusOne istio.Instance
	NMinusTwo istio.Instance
	apps      VersionedEchoDeployments
)

// TestMain sets up revisions on various versions as the apps
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&Latest, func(ctx resource.Context, cfg *istio.Config) {
		})).
		Setup(istio.Setup(&NMinusOne, func(ctx resource.Context, cfg *istio.Config) {
			cfg.Version = "1.7.6"
			cfg.Revision = "1-7-6"
		})).
		Setup(istio.Setup(&NMinusTwo, func(ctx resource.Context, cfg *istio.Config) {
			cfg.Version = "1.8.0"
			cfg.Revision = "1-8-0"
		})).
		Setup(func(ctx resource.Context) error {
			log.Println("-=-=-=-SETTING UP VERSIONED APPS-=-=-=-")
			return SetupApps(ctx, Latest, NMinusOne, NMinusTwo, &apps)
		}).
		Run()
}
