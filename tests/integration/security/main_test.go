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

	cfg.ControlPlaneValues = `
values:
  pilot: 
    jwksResolverExtraRootCA: |
      MIIDRTCCAi2gAwIBAgIUBeTBvkDJcwG0pIYWP/VphzEGWVEwDQYJKoZIhvcNAQEL
      BQAwRjELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkFaMRMwEQYDVQQKDApBY21lLCBJ
      bmMuMRUwEwYDVQQDDAxBY21lIFJvb3QgQ0EwHhcNMjExMDI5MTYwNjEwWhcNMjIx
      MDI5MTYwNjEwWjA/MQswCQYDVQQGEwJVUzELMAkGA1UECAwCQVoxEzARBgNVBAoM
      CkFjbWUsIEluYy4xDjAMBgNVBAMMBSouY29tMIIBIjANBgkqhkiG9w0BAQEFAAOC
      AQ8AMIIBCgKCAQEApJ5TZ4wD/J4GNbybLUjZ3I22ydogI+pEivuomTlqqJVoQVJl
      r6HtmRQ+Ii5YFBsKoJt7H94T4mO3kC9f6JHyc/na3BS8ZK1S43rps6p2jk7/iU8w
      BqF344YVFLdaij7cIm0ZsMu0ZegoLMewOCGoEDd6E4vybXMsJP2UtB1+p/IcX8ba
      S7hOjpoIV1Kmz4esyn39o0Zv4ROad07tWKM3OhcSEPY0A8Cr6th0V62UL/AGHqec
      VKUzFQMNX5nxYJOyY0rdjOB1LeMIn72Z2v8wuVT9iiDGVaeSgRAM9IRlF+6bROG5
      kIIdxTK+jXRJd0Os5oDw5e4L7z5mGQhOT/uhOwIDAQABozIwMDAuBgNVHREEJzAl
      gg8qLmxvY2FsaG9zdC5jb22CByoubG9jYWyCCWxvY2FsaG9zdDANBgkqhkiG9w0B
      AQsFAAOCAQEAMobpQMkO8K4rNVxcNlCBek/8t6kgSuRDQkylSF2ozzPa9tLVkyep
      PhA8wmryr3AY7qHf1Tybys3ZByuu6RBCivq8GqgqvjZWroczQcvzcQForhgf2gvG
      ePZ2qOxyNe5N1gaXG1afJuzGay5SEG70kQJS2z97Cpn+Kioca70gpM4ZySUcMTuK
      tqi3sxpuv0FSvpWN2il7FB8+3XZOkSqM6aaAY6ZS2l5an8aV+x6Y7HqfEXG5Jefb
      1Exd1p2jG1mtLzQ7g9AC53fyjV2Uef0F97GkxjyRXBMH7rnsYMa09vc8zTX1GmCs
      Yayl8f9F8jPeWWg7dg0OnH0X2sssgQIVdw==
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: true
meshConfig:
  accessLogEncoding: JSON
  accessLogFile: /dev/stdout
  defaultConfig:
    gatewayTopology:
      numTrustedProxies: 1`
}
