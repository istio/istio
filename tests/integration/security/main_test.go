//go:build integ
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

package security

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/pkg/log"
)

var (
	ist  istio.Instance
	apps = &util.EchoDeployments{}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&ist, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return util.SetupApps(ctx, ist, apps, !ctx.Settings().Skip(echo.VM))
		}).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	controlPlaneValues := `
values:
  pilot: 
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: true
meshConfig:
  accessLogFile: /dev/stdout
  defaultConfig:
    image:
      imageType: "%s"
    gatewayTopology:
      numTrustedProxies: 1`

	imageType := "default"
	if strings.HasSuffix(ctx.Settings().Image.Tag, "-distroless") {
		imageType = "distroless"
	}

	val := fmt.Sprintf(controlPlaneValues, imageType)
	log.Infof("controlPlaneValues %v + %v ==> %v ", controlPlaneValues, imageType, val)

	cfg.ControlPlaneValues = val
}
