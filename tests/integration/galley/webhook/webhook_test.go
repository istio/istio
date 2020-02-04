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
// limitations under the License.

package galley

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/webhooks/validation/server"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	tkube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	webhookControllerApp string
	deployName           string
	clusterRolePrefix    string
	vwcName              string
	sleepDelay           = 10 * time.Second // How long to wait to give the reconcile loop an opportunity to act
	i                    istio.Instance
)

func setup(cfg *istio.Config) {
	if cfg.IsIstiodEnabled() {
		webhookControllerApp = "pilot" // i.e. istiod. Switch to 'galley' and change the setup options for non-istiod tests.
		deployName = "istiod"
		clusterRolePrefix = "istiod"
		vwcName = "istiod-istio-system"
	} else {
		webhookControllerApp = "galley"
		deployName = fmt.Sprintf("istio-%v", webhookControllerApp)
		clusterRolePrefix = "istio-galley"
		vwcName = "istio-galley"
	}
}

// Using subtests to enforce ordering
func TestWebhook(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			env := ctx.Environment().(*kube.Environment)

			istioNs := i.Settings().IstioNamespace

			// Verify the controller re-creates the webhook when it is deleted.
			ctx.NewSubTest("recreateOnDelete").
				Run(func(t framework.TestContext) {
					startVersion := getVwcResourceVersion(vwcName, t, env)

					if err := env.DeleteValidatingWebhook(vwcName); err != nil {
						t.Fatalf("Error deleting validation webhook: %v", err)
					}

					time.Sleep(sleepDelay)
					version := getVwcResourceVersion(vwcName, t, env)
					if version == startVersion {
						t.Fatalf("ValidatingWebhookConfiguration was not re-created after deletion")
					}
				})

			// Verify that scaling up/down doesn't modify webhook configuration
			ctx.NewSubTest("scaling").
				Run(func(t framework.TestContext) {
					startGen := getVwcGeneration(vwcName, t, env)

					// Scale up
					scaleDeployment(istioNs, deployName, 2, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen := getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up to 2")
					}

					// Scale down to zero
					scaleDeployment(istioNs, deployName, 0, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale down to zero")
					}

					// Scale back to 1
					scaleDeployment(istioNs, deployName, 1, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up back to 1")
					}
				})

			// Verify that the webhook's key and cert are reloaded, e.g. on rotation
			ctx.NewSubTest("key/cert reload").
				Run(func(t framework.TestContext) {
					cfg := i.Settings()
					if cfg.IsIstiodEnabled() {
						t.Skip("istiod does not support cert rotation")
					}

					addr, done := startWebhookPortForwarderOrFail(t, env, cfg)
					defer done()

					startingSN := fetchWebhookCertSerialNumbersOrFail(t, addr)

					log.Infof("Initial cert serial numbers: %v", startingSN)

					_ = env.DeleteSecret(istioNs, fmt.Sprintf("istio.%v-service-account", deployName))

					retry.UntilSuccessOrFail(t, func() error {
						updated := fetchWebhookCertSerialNumbersOrFail(t, addr)
						if diff := cmp.Diff(startingSN, updated); diff != "" {
							log.Infof("Updated cert serial numbers: %v", updated)
							return nil
						}
						return errors.New("no change to cert serial numbers")
					}, retry.Timeout(5*time.Minute))
				})

			// NOTE: Keep this as the last test! It deletes the webhook's clusterrole. All subsequent kube tests will fail.
			// Verify that removing clusterrole results in the webhook configuration being removed.
			ctx.NewSubTest("webhookUninstall").
				Run(func(t framework.TestContext) {
					env.DeleteClusterRole(fmt.Sprintf("%v-%v", clusterRolePrefix, istioNs))

					// Verify webhook config is deleted
					if err := env.WaitForValidatingWebhookDeletion(vwcName); err != nil {
						t.Fatal(err)
					}
				})
		})
}

func scaleDeployment(namespace, deployment string, replicas int, t test.Failer, env *kube.Environment) {
	if err := env.ScaleDeployment(namespace, deployment, replicas); err != nil {
		t.Fatalf("Error scaling deployment %s to %d: %v", deployment, replicas, err)
	}
	if err := env.WaitUntilDeploymentIsReady(namespace, deployment); err != nil {
		t.Fatalf("Error waiting for deployment %s to be ready: %v", deployment, err)
	}

	// verify no pods are still terminating
	if replicas == 0 {
		fetchFunc := env.Accessor.NewSinglePodFetch(namespace, fmt.Sprintf("app=%v", webhookControllerApp))
		retry.UntilSuccessOrFail(t, func() error {
			pods, err := fetchFunc()
			if err != nil {
				if k8serrors.IsNotFound(err) || strings.Contains(err.Error(), "no matching pod found for selectors") {
					return nil
				}
				return err
			}
			if len(pods) > 0 {
				return fmt.Errorf("%v pods remaining", len(pods))
			}
			return nil
		}, retry.Timeout(5*time.Minute))
	}
}

func getVwcGeneration(vwcName string, t test.Failer, env *kube.Environment) int64 {
	t.Helper()

	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetGeneration()
}

func getVwcResourceVersion(vwcName string, t test.Failer, env *kube.Environment) string {
	t.Helper()

	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetResourceVersion()
}

func startWebhookPortForwarderOrFail(t test.Failer, env *kube.Environment, cfg istio.Config) (addr string, done func()) {
	t.Helper()

	// This function should only be called when testing galley's validation
	// webhook. Istiod doesn't support cert rotation.
	if cfg.IsIstiodEnabled() {
		t.Fatal("istiod does not support cert rotation")
	}
	// Hardcode Galley's webhook port.
	port := uint16(server.DefaultArgs().Port)

	// ensure only one pod *exists* before we start port forwarding.
	scaleDeployment(cfg.IstioNamespace, deployName, 0, t, env)
	scaleDeployment(cfg.IstioNamespace, deployName, 1, t, env)

	var webhookPod *v1.Pod
	fetchFunc := env.Accessor.NewSinglePodFetch(cfg.IstioNamespace, fmt.Sprintf("app=%v", webhookControllerApp))
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := fetchFunc()
		if err != nil {
			return err
		}
		if len(pods) != 1 {
			return fmt.Errorf("%v pods found, waiting for only one", len(pods))
		}
		webhookPod = &pods[0]
		return tkube.CheckPodReady(webhookPod)
	}, retry.Timeout(5*time.Minute))

	forwarder, err := env.Accessor.NewPortForwarder(*webhookPod, 0, port)
	if err != nil {
		t.Fatalf("failed creating port forwarding to the controller: %v", err)
	}

	if err := forwarder.Start(); err != nil {
		t.Fatalf("failed to start port forwarding: %v", err)

	}

	done = func() {
		if err := forwarder.Close(); err != nil {
			t.Fatalf("An error occurred when the port forwarder was closed: %v", err)
		}
	}
	return forwarder.Address(), done
}

func fetchWebhookCertSerialNumbersOrFail(t test.Failer, addr string) []string { // nolint: interfacer
	t.Helper()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			DialContext: (&net.Dialer{
				Timeout: 30 * time.Second,
			}).DialContext,
		},
	}
	defer client.CloseIdleConnections()

	url := fmt.Sprintf("https://%v/%v", addr, server.HTTPSHandlerReadyPath)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		t.Fatalf("invalid request: %v", err)
	}

	req.Host = fmt.Sprintf("%v.istio-system.svc", deployName)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("webhook request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.TLS == nil {
		t.Fatal("server did not provide certificates")
	}

	var sn []string
	for _, cert := range resp.TLS.PeerCertificates {
		sn = append(sn, cert.SerialNumber.Text(16))
	}
	sort.Strings(sn)
	return sn
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("webhook_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&i, setup)).
		Run()
}
