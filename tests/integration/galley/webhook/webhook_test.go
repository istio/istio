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
	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	tkube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/webhooks/validation/server"
)

var (
	webhookControllerApp string
	deployName           string
	vwcName              string
	i                    istio.Instance
)

func setup(cfg *istio.Config) {
	if cfg.IsIstiodEnabled() {
		webhookControllerApp = "pilot" // i.e. istiod. Switch to 'galley' and change the setup options for non-istiod tests.
		deployName = "istiod"
		vwcName = "istiod-istio-system"
	} else {
		webhookControllerApp = "galley"
		deployName = fmt.Sprintf("istio-%v", webhookControllerApp)
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

			ctx.NewSubTest("update").
				Run(func(ctx framework.TestContext) {
					env := ctx.Environment().(*kube.Environment)

					// clear the updated fields and verify istiod updates them

					retry.UntilSuccessOrFail(ctx, func() error {
						got, err := env.GetValidatingWebhookConfiguration(vwcName)
						if err != nil {
							return fmt.Errorf("error getting initial webhook: %v", err)
						}
						if err := verifyValidatingWebhookConfiguration(got); err != nil {
							return err
						}

						updated := got.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
						updated.Webhooks[0].ClientConfig.CABundle = nil
						ignore := kubeApiAdmission.Ignore // can't take the address of a constant
						updated.Webhooks[0].FailurePolicy = &ignore

						return env.UpdateValidatingWebhookConfiguration(updated)
					})

					retry.UntilSuccessOrFail(ctx, func() error {
						got, err := env.GetValidatingWebhookConfiguration(vwcName)
						if err != nil {
							t.Fatalf("error getting initial webhook: %v", err)
						}
						if err := verifyValidatingWebhookConfiguration(got); err != nil {
							return fmt.Errorf("validatingwebhookconfiguration not updated yet: %v", err)
						}
						return nil
					})
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

					retry.UntilSuccessOrFail(ctx, func() error {
						updated := fetchWebhookCertSerialNumbersOrFail(t, addr)
						if diff := cmp.Diff(startingSN, updated); diff != "" {
							log.Infof("Updated cert serial numbers: %v", updated)
							return nil
						}
						return errors.New("no change to cert serial numbers")
					}, retry.Timeout(5*time.Minute))
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

func verifyValidatingWebhookConfiguration(c *kubeApiAdmission.ValidatingWebhookConfiguration) error {
	if len(c.Webhooks) == 0 {
		return errors.New("no webhook entries found")
	}
	for i, wh := range c.Webhooks {
		if *wh.FailurePolicy != kubeApiAdmission.Fail {
			return fmt.Errorf("webhook #%v: wrong failure policy. c %v wanted %v",
				i, *wh.FailurePolicy, kubeApiAdmission.Fail)
		}
		if len(wh.ClientConfig.CABundle) == 0 {
			return fmt.Errorf("webhook #%v: caBundle not matched", i)
		}
	}
	return nil
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("webhook_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&i, setup)).
		Run()
}
