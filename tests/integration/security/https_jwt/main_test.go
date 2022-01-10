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
	"path"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/cert"
)

var (
	ist  istio.Instance
	apps = &util.EchoDeployments{}
)

func loadCert(filename string) (string, error) {
	data, err := cert.ReadSampleCertFromFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&ist, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return util.SetupApps(ctx, ist, apps, true)
		}).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	script := path.Join(env.IstioSrc, "samples/jwt-server/testdata", "ca.crt")
	rootCaCert, err := loadCert(script)
	if err != nil {
		return
	}
	// command to generate certificate
	// use the generated ca.crt by following https://github.com/istio/istio/blob/master/samples/jwt-server/testdata/README.MD
	// TODO(garyan): enable the test for "PILOT_JWT_ENABLE_REMOTE_JWKS: true" as well.
	cfg.ControlPlaneValues = tmpl.MustEvaluate(`
values:
  pilot: 
    jwksResolverExtraRootCA: |
{{.pem | indent 6}}
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: false
meshConfig:
  accessLogFile: /dev/stdout`, map[string]string{"pem": rootCaCert})
}
