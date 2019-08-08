// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
package tests

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/istio.io/examples"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework.NewSuite("authn-policy", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, setupConfig)).
		RequireEnvironment(environment.Kube).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// This is redundant, but setting it explicitly to match the docs as it's explicitly required
	// in the docs.
	cfg.Values["global.mtls.enabled"] = "false"
}

// https://preliminary.istio.io/docs/tasks/security/authn-policy/
// https://github.com/istio/istio.io/blob/master/content/docs/tasks/security/authn-policy/index.md
func TestAuthnPolicy(t *testing.T) {
	ex := examples.New(t, "Setup")

	ex.AddScript("", "create-namespaces.sh", examples.TextOutput)
	ex.AddFile("foo", "samples/httpbin/httpbin.yaml")
	ex.AddFile("foo", "samples/sleep/sleep.yaml")
	ex.AddFile("bar", "samples/httpbin/httpbin.yaml")
	ex.AddFile("bar", "samples/sleep/sleep.yaml")
	ex.AddFile("legacy", "samples/httpbin/httpbin.yaml")
	ex.AddFile("legacy", "samples/sleep/sleep.yaml")

	// This is missing from the docs, but it is necessary before continuing.
	ex.AddScript("", "wait-for-containers.sh", examples.TextOutput)
	ex.AddScript("", "verify-reachability.sh", examples.TextOutput)

	// TODO: Update the docs to use commands that succeed or fail, to check the authentication
	//  policies and destination rules, and use the same commands here.
	ex.Run()

	ex = examples.New(t, "Globally enabling Istio mutual TLS")

	ex.AddScript("", "part1-configure-authentication-meshpolicy.sh", examples.TextOutput)
	// TODO: Update the docs to add instructions to wait until the policy has been propagated,
	//  and use the same commands here.

	// TODO: Check the output of the command. Fail if curl doesn't fail.
	ex.AddScript("", "part1-verify-reachability-from-istio.sh", examples.TextOutput)
	ex.AddScript("", "part1-configure-destinationrule-default.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part1-verify-reachability-from-istio.sh", examples.TextOutput)
	// TODO: Fail if curl doesn't fail.
	ex.AddScript("", "part1-verify-reachability-from-non-istio.sh", examples.TextOutput)

	// TODO: Fail if curl doesn't fail.
	ex.AddScript("", "part1-verify-reachability-to-legacy.sh", examples.TextOutput)
	ex.AddScript("", "part1-configure-destinationrule-httpbin-legacy.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part1-verify-reachability-to-legacy.sh", examples.TextOutput)

	// TODO: Fail if curl doesn't fail.
	ex.AddScript("", "part1-verify-reachability-to-k8s-api.sh", examples.TextOutput)
	ex.AddScript("", "part1-configure-destinationrule-api-server.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part1-verify-reachability-to-k8s-api.sh", examples.TextOutput)

	ex.AddScript("", "part1-cleanup.sh", examples.TextOutput)

	ex.Run()

	ex = examples.New(t, "Enable mutual TLS per namespace or service")

	ex.AddScript("", "part2-configure-authentication-policy-default.sh", examples.TextOutput)
	ex.AddScript("", "part2-configure-destinationrule-default.sh", examples.TextOutput)
	// TODO: Update the docs to add instructions to wait until the policy has been propagated,
	//  and use the same commands here.

	// TODO: Fail if curl from foo or bar to any other namespace fails.
	// TODO: Fail if curl from legacy to foo succeeds.
	ex.AddScript("", "part2-verify-reachability.sh", examples.TextOutput)
	ex.AddScript("", "part2-configure-authentication-policy-httpbin.sh", examples.TextOutput)
	ex.AddScript("", "part2-configure-destinationrule-httpbin.sh", examples.TextOutput)
	// TODO: Fail if curl from foo or bar to any other namespace fails.
	// TODO: Fail if curl from legacy to foo OR bar succeeds.
	ex.AddScript("", "part2-verify-reachability.sh", examples.TextOutput)

	ex.AddScript("", "part2-configure-authentication-policy-httpbin-port.sh", examples.TextOutput)
	ex.AddScript("", "part2-configure-destinationrule-httpbin-port.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part2-verify-reachability-to-bar-port-8000.sh", examples.TextOutput)

	ex.AddScript("", "part2-configure-authentication-policy-overwrite-example.sh", examples.TextOutput)
	ex.AddScript("", "part2-configure-destinationrule-overwrite-example.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part2-verify-reachability-to-foo-port-8000.sh", examples.TextOutput)

	ex.AddScript("", "part2-cleanup.sh", examples.TextOutput)

	ex.Run()

	ex = examples.New(t, "End-user authentication")

	ex.AddScript("", "part3-configure-gateway-httpbin.sh", examples.TextOutput)
	ex.AddScript("", "part3-configure-virtualservice-httpbin.sh", examples.TextOutput)
	// TODO: Update the docs to add instructions to wait until the gateway is ready,
	//  and use the same commands here.

	// TODO: Fail if curl fails.
	ex.AddScript("", "part3-verify-reachability-headers-without-token.sh", examples.TextOutput)
	ex.AddScript("", "part3-configure-authentication-policy-jwt-example.sh", examples.TextOutput)
	// TODO: Fail if curl succeeds.
	ex.AddScript("", "part3-verify-reachability-headers-without-token.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part3-verify-reachability-headers-with-token.sh", examples.TextOutput)

	// TODO: Add the test that runs security/tools/jwt/samples/gen-jwt.py against
	//  security/tools/jwt/samples/key.pem.
	//  This requires having Python and the jwcrypto library installed locally.

	ex.AddScript("", "part3-configure-authentication-policy-jwt-example-exclude.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part3-verify-reachability-useragent-without-token.sh", examples.TextOutput)
	// TODO: Fail if curl succeeds.
	ex.AddScript("", "part3-verify-reachability-headers-without-token.sh", examples.TextOutput)

	ex.AddScript("", "part3-configure-authentication-policy-jwt-example-include.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part3-verify-reachability-useragent-without-token.sh", examples.TextOutput)
	// TODO: Fail if curl succeeds.
	ex.AddScript("", "part3-verify-reachability-ip-without-token.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part3-verify-reachability-ip-with-token.sh", examples.TextOutput)

	ex.AddScript("", "part3-configure-authentication-policy-jwt-mtls.sh", examples.TextOutput)
	ex.AddScript("", "part3-configure-destinationrule-httpbin.sh", examples.TextOutput)
	// TODO: Fail if curl fails.
	ex.AddScript("", "part3-verify-reachability-from-istio-with-token.sh", examples.TextOutput)
	// TODO: Fail if curl succeeds.
	ex.AddScript("", "part3-verify-reachability-from-non-istio-with-token.sh", examples.TextOutput)

	ex.AddScript("", "part3-cleanup.sh", examples.TextOutput)

	ex.Run()
}
