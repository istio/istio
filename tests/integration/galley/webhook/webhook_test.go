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

	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	tkube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

var (
	vwcName    = "istio-galley"
	deployName = "istio-galley"
	sleepDelay = 10 * time.Second // How long to wait to give the reconcile loop an opportunity to act
)

var i istio.Instance

// Using subtests to enforce ordering
func TestWebhook(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			env := ctx.Environment().(*kube.Environment)

			istioNs := i.Settings().IstioNamespace

			// Verify galley re-creates the webhook when it is deleted.
			ctx.NewSubTest("recreateOnDelete").
				Run(func(ctx framework.TestContext) {
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
				Run(func(ctx framework.TestContext) {
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
				Run(func(ctx framework.TestContext) {
					addr, done := startGalleyPortForwarderOrFail(t, env, istioNs)
					defer done()

					startingSN := fetchWebhookCertSerialNumbersOrFail(t, addr)

					log.Infof("Initial cert serial numbers: %v", startingSN)

					env.DeleteSecret(istioNs, "istio.istio-galley-service-account")

					retry.UntilSuccessOrFail(t, func() error {
						updated := fetchWebhookCertSerialNumbersOrFail(t, addr)
						if diff := cmp.Diff(startingSN, updated); diff != "" {
							log.Infof("Updated cert serial numbers: %v", updated)
							return nil
						}
						return errors.New("no change to cert serial numbers")
					}, retry.Timeout(5*time.Minute))
				})

			// NOTE: Keep this as the last test! It deletes the istio-system namespaces. All subsequent kube tests will fail.
			// Verify that removing galley's namespace results in the webhook configuration being removed
			ctx.NewSubTest("webhookUninstall").
				Run(func(ctx framework.TestContext) {
					// Remove galley's namespace
					env.DeleteNamespace(istioNs)

					// Verify webhook config is deleted
					env.WaitForValidatingWebhookDeletion(vwcName)
				})
		})
}

func scaleDeployment(namespace, deployment string, replicas int, t *testing.T, env *kube.Environment) {
	if err := env.ScaleDeployment(namespace, deployment, replicas); err != nil {
		t.Fatalf("Error scaling deployment %s to %d: %v", deployment, replicas, err)
	}
	if err := env.WaitUntilDeploymentIsReady(namespace, deployment); err != nil {
		t.Fatalf("Error waiting for deployment %s to be ready: %v", deployment, err)
	}

	// verify no pods are still terminating
	if replicas == 0 {
		fetchFunc := env.Accessor.NewSinglePodFetch(namespace, "app=galley")
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
		}, retry.Timeout(5*time.Minute)) // galley
	}
}

func getVwcGeneration(vwcName string, t *testing.T, env *kube.Environment) int64 {
	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetGeneration()
}

func getVwcResourceVersion(vwcName string, t *testing.T, env *kube.Environment) string {
	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetResourceVersion()
}

func startGalleyPortForwarderOrFail(t *testing.T, env *kube.Environment, ns string) (addr string, done func()) {
	t.Helper()

	// ensure only one pod *exists* before we start port forwarding.
	scaleDeployment(ns, deployName, 0, t, env)
	scaleDeployment(ns, deployName, 1, t, env)

	var galleyPod *v1.Pod
	fetchFunc := env.Accessor.NewSinglePodFetch(ns, "app=galley")
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := fetchFunc()
		if err != nil {
			return err
		}
		if len(pods) != 1 {
			return fmt.Errorf("%v pods found, waiting for only one", len(pods))
		}
		galleyPod = &pods[0]
		return tkube.CheckPodReady(galleyPod)
	}, retry.Timeout(5*time.Minute))

	forwarder, err := env.Accessor.NewPortForwarder(*galleyPod, 0, uint16(validation.DefaultArgs().Port))
	if err != nil {
		t.Fatalf("failed creating port forwarding to galley: %v", err)
	}

	if err := forwarder.Start(); err != nil {
		t.Fatalf("failed to start port forwarding: %v", err)

	}

	done = func() {
		if err := forwarder.Close(); err != nil {
			t.Errorf("An error occurred when the port forwarder was closed: %v", err)
		}
	}
	return forwarder.Address(), done
}

func fetchWebhookCertSerialNumbersOrFail(t *testing.T, addr string) []string { // nolint: interfacer
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

	url := fmt.Sprintf("https://%v/ready", addr)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		t.Fatalf("iunvalid request: %v", err)
	}

	req.Host = "istio-galley.istio-system.svc"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("webhook request failed: %v", err)
	}
	resp.Body.Close()

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
		SetupOnEnv(environment.Kube, istio.Setup(&i, nil)).
		Run()
}
