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
	// use the generated ca.crt by following https://github.com/istio/istio/blob/master/samples/jwt-server/testdata/README.MD
	cfg.ControlPlaneValues = `
values:
  pilot: 
    jwksResolverExtraRootCA: |
      -----BEGIN CERTIFICATE-----
      MIIDbTCCAlWgAwIBAgIUbjEHNUqX2coTbuqeqGy1x3XQTQcwDQYJKoZIhvcNAQEL
      BQAwRjELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkFaMRMwEQYDVQQKDApBY21lLCBJ
      bmMuMRUwEwYDVQQDDAxBY21lIFJvb3QgQ0EwHhcNMjExMTE3MDQ0MjQyWhcNMzEx
      MTE1MDQ0MjQyWjBGMQswCQYDVQQGEwJVUzELMAkGA1UECAwCQVoxEzARBgNVBAoM
      CkFjbWUsIEluYy4xFTATBgNVBAMMDEFjbWUgUm9vdCBDQTCCASIwDQYJKoZIhvcN
      AQEBBQADggEPADCCAQoCggEBANAulWo5mZuyh/goX5QnbXK/mdGYJBHZQyIsuRs3
      Lj3cR4u/eZ5zrS1biKbb5rqO0yypA6pou91dOsr+BVi/E+c4SF/IwmSfPcTYABJV
      RRdNv8iHV/RhstAiWPF8nL7gjYv2oa+Y4Oq4ZZRHkew+mtnpXV6U7yKnYp+zXZxf
      clgLi+ubzdBfcowfPVPKblwFj9Jx3FSXeE27xVhKDVXrpNnULxX1r0UcGlvPnC4q
      AnmhuP+/oQDoYxBak6lD5UBGhck0jMCxXa13HhvWwKzJK/iFJi3vkeqDrKYnlplk
      sJZfE6yod1FxIYUYlu2Z4kIg94/qM5Do3rzyW4WJdVgr5AUCAwEAAaNTMFEwHQYD
      VR0OBBYEFC1ZGL86V16787hsljSiV7IHsmF0MB8GA1UdIwQYMBaAFC1ZGL86V167
      87hsljSiV7IHsmF0MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
      AH6ybGMGwM3cXqLbZtR1ayJJhJx/v2CWK6WOc7T73nm+oQWyb5byvfMPa4G7JRmc
      XWQbIoB9w5Gu62Vo3CFCleLq7uSKKjKw8XNMpKNm2MsL7pKPJs13XbjXRoyn/OP3
      IjrAi3+wks5Wt8BhwJDp1JJLiRF1FGKLquuO7alm8zAs6PSXRePDPXQdSfDCGy+q
      StG8hekFKF+BomUBRbuuMjSKPlnD3eMIdmWXHsTW8Gg1Anua7ddKD9aZUAxbNHk4
      31paKAmE2v8g/ZUtFKYLJyFhVqe8pB6IhQZm3/3C8xvJpy1sTj7u+ydqkzHbIUfh
      Iaw5/US65AS6weIJcztH2Zw=
      -----END CERTIFICATE-----
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: false
meshConfig:
  accessLogFile: /dev/stdout`
}
