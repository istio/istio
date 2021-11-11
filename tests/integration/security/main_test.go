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
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
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
			return util.SetupApps(ctx, ist, apps, true)
		}).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// command to generate certificate
	// https://github.com/istio/istio/blob/master/samples/jwt-server/testdata/README.MD
	cfg.ControlPlaneValues = `
values:
  pilot: 
    jwksResolverExtraRootCA: |
      MIIDNzCCAh+gAwIBAgIUaWsrhw0rt8BTBrx3yLpCJEc2BM0wDQYJKoZIhvcNAQEL
      BQAwRjELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkFaMRMwEQYDVQQKDApBY21lLCBJ
      bmMuMRUwEwYDVQQDDAxBY21lIFJvb3QgQ0EwHhcNMjExMTEwMjExODMwWhcNMjIx
      MTEwMjExODMwWjA/MQswCQYDVQQGEwJVUzELMAkGA1UECAwCQVoxEzARBgNVBAoM
      CkFjbWUsIEluYy4xDjAMBgNVBAMMBSouY29tMIIBIjANBgkqhkiG9w0BAQEFAAOC
      AQ8AMIIBCgKCAQEA5IovdxgVfwqhk7HnGNTdhtNnKLeqsfNy15XUK/gWRAMnY8Im
      d8l+b2/QCk+zZvn8xf8ATe5klpMP85yuG8xi97pv2fZhzvZCbUyR9CJqnA5FQe7i
      itmrL7velvIHJpqdyxVUaUNPOII0NNL1w8ZDHNymNycc+BQZcGvXnRO66EXiBSv1
      346o4MX/qQ7T7WdPwKw6+8dQ1T55n3pA3mGeVCUfVlWG/3vh/uOmD/eZjLn68JWL
      VmYl9QY4UFbYsAmN4RCcNLDyTHoJ/QAenHlBOQ9ogIixH9nhQoyN1aspRjECoP0N
      33LoV2ugYK6+slqrTlC/i9IzBg5Fe6ld6PirpwIDAQABoyQwIjAgBgNVHREEGTAX
      ggpqd3Qtc2VydmVygglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggEBAFHjOubE
      rvkIMirO4iQFfMEyz5DvgLCXAfCLyPJvnYc2sgupuLypwWlmm/N2+iHBOwfJUGWS
      sV7LOVy63RV+Gg/CjDnNNSf6EZcEJpXQRKzC1QZ+5vzDIYQ+Agegm3CEaOBKm8Qh
      U+r+Uang1iw5Gxr0MMBIwmV4AZlkYOLnBgsAx0G4L4wkWEnSHsV7edcr1epU8Y3S
      OBWIJNY95iwFtLPrlUDYenxAjU8IlntlM5iF3hvqdZGObdpQQG3xuXNhHi7gEULN
      Z4s9kB70016WKXl5hgxnQt5Tt5BMKQAFuMoqqGLaG6I/OTS+ujHObHmGxebok9YH
      YuKt86BGxZynxsA=
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: true
meshConfig:
  accessLogEncoding: JSON
  accessLogFile: /dev/stdout
  defaultConfig:
    gatewayTopology:
      numTrustedProxies: 1`
}
