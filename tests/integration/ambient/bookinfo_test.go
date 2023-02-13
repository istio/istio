//go:build integ

// Copyright Istio Authors
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

package ambient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	bookinfoDir     = "../../../samples/bookinfo/"
	bookinfoFile    = bookinfoDir + "platform/kube/bookinfo.yaml"
	defaultDestRule = bookinfoDir + "networking/destination-rule-all.yaml"
	bookinfoGateway = bookinfoDir + "networking/bookinfo-gateway.yaml"
	routingV1       = bookinfoDir + "networking/virtual-service-all-v1.yaml"
	headerRouting   = bookinfoDir + "networking/virtual-service-reviews-test-v2.yaml"
	templateFile    = "testdata/modified-waypoint-template.yaml"
)

func TestBookinfo(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			skipsForTest(t)
			nsConfig, err := namespace.New(t, namespace.Config{
				Prefix: "bookinfo",
				Inject: false,
				Labels: map[string]string{
					constants.DataplaneMode: "ambient",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			setupBookinfo(t, nsConfig)
			applyDefaultRouting(t, nsConfig)

			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(fmt.Errorf("Got error while creating cookie jar %s", err.Error()))
			}
			ingressClient := http.Client{}
			ingressInst := istio.DefaultIngressOrFail(t, t)
			addr, ingrPort := ingressInst.HTTPAddress()
			ingressURL := fmt.Sprintf("http://%v:%v", addr, ingrPort)

			t.NewSubTest("no waypoint").Run(func(t framework.TestContext) {
				t.NewSubTest("productpage reachable").Run(func(t framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := ingressClient.Get(ingressURL + "/productpage")
						if err != nil {
							return fmt.Errorf("error fetching /productpage: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}
						bodyBytes, err := io.ReadAll(resp.Body)
						if err != nil {
							return fmt.Errorf("error reading /productpage response: %v", err)
						}
						reviewsFound := strings.Contains(string(bodyBytes), "Reviews served by:")
						detailsFound := strings.Contains(string(bodyBytes), "Book Details")
						if !reviewsFound || !detailsFound {
							return fmt.Errorf("productpage could not reach other service(s), reviews reached:%v details reached:%v", reviewsFound, detailsFound)
						}
						return nil
					})
				})
			})

			t.NewSubTest("waypoint routing").Run(func(t framework.TestContext) {
				setupWaypoints(t, nsConfig)

				t.NewSubTest("productpage reachable").Run(func(t framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := ingressClient.Get(ingressURL + "/productpage")
						if err != nil {
							return fmt.Errorf("error fetching /productpage: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}
						bodyBytes, err := io.ReadAll(resp.Body)
						if err != nil {
							return fmt.Errorf("error reading /productpage response: %v", err)
						}
						reviewsFound := strings.Contains(string(bodyBytes), "Reviews served by:")
						detailsFound := strings.Contains(string(bodyBytes), "Book Details")
						if !reviewsFound || !detailsFound {
							return fmt.Errorf("productpage could not reach other service(s), reviews reached:%v details reached:%v", reviewsFound, detailsFound)
						}
						return nil
					})
				})

				t.NewSubTest("reviews v1").Run(func(t framework.TestContext) {
					applyFileOrFail(t, nsConfig.Name(), routingV1)
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := ingressClient.Get(ingressURL + "/productpage")
						if err != nil {
							return fmt.Errorf("error fetching /productpage: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}
						bodyBytes, err := io.ReadAll(resp.Body)
						if err != nil {
							return fmt.Errorf("error reading /productpage response: %v", err)
						}
						if !strings.Contains(string(bodyBytes), "Reviews served by:") {
							return fmt.Errorf("productpage could not reach reviews")
						}
						if strings.Contains(string(bodyBytes), "glyphicon glyphicon-star") {
							return fmt.Errorf("stars were provided when none were exected")
						}
						return nil
					}, retry.Converge(5))
				})
				t.NewSubTest("reviews v2").Run(func(t framework.TestContext) {
					applyFileOrFail(t, nsConfig.Name(), headerRouting)
					cookieClient := http.Client{
						Jar: jar,
						CheckRedirect: func(req *http.Request, via []*http.Request) error {
							return http.ErrUseLastResponse
						},
					}
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := cookieClient.PostForm(ingressURL+"/login",
							url.Values{"username": {"jason"}, "passwd": {"password"}})
						if err != nil {
							return fmt.Errorf("error during /login: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusFound {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}

						resp, err = cookieClient.Get(ingressURL + "/productpage")
						if err != nil {
							return fmt.Errorf("error fetching /productpage: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}
						bodyBytes, err := io.ReadAll(resp.Body)
						if err != nil {
							return fmt.Errorf("error reading /productpage response: %v", err)
						}
						if !strings.Contains(string(bodyBytes), "glyphicon glyphicon-star") {
							return fmt.Errorf("expected stars to be provided with reviews, received none. Body:\n%v", string(bodyBytes))
						}
						return nil
					})
				})
			})
			t.NewSubTest("waypoint template change").Run(func(t framework.TestContext) {
				// check that waypoint deployment is unmodified
				g := gomega.NewGomegaWithT(t)
				pod, err := kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), constants.GatewayNameLabel+"=bookinfo-waypoints")()
				g.Expect(err).NotTo(gomega.HaveOccurred())
				haveEnvVar := gomega.ContainElement(gomega.BeEquivalentTo(v1.EnvVar{Name: "FOO", Value: "bar"}))
				g.Expect(pod[0].Spec.Containers[0].Env).NotTo(haveEnvVar)
				// modify template
				istio.GetOrFail(t, t).UpdateInjectionConfig(t, func(c *inject.Config) error {
					c.RawTemplates["waypoint"] = file.MustAsString(templateFile)
					return nil
				}, cleanup.Conditionally)
				// wait to see modified waypoint deployment
				getPodEnvVars := func() []v1.EnvVar {
					pods, err := kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), constants.GatewayNameLabel+"=bookinfo-waypoints")()
					g.Expect(err).NotTo(gomega.HaveOccurred())
					var result []v1.EnvVar
					// if old pods haven't shut down yet, include all pods env vars
					for _, pod := range pods {
						result = append(result, pod.Spec.Containers[0].Env...)
					}
					return result
				}
				g.Eventually(getPodEnvVars, time.Minute).Should(haveEnvVar)
			})
		})
}

func applyDefaultRouting(t framework.TestContext, nsConfig namespace.Instance) {
	applyFileOrFail(t, nsConfig.Name(), defaultDestRule)
	applyFileOrFail(t, nsConfig.Name(), bookinfoGateway)
	applyFileOrFail(t, nsConfig.Name(), routingV1)
}

func setupWaypoints(t framework.TestContext, nsConfig namespace.Instance) {
	istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
		"x",
		"waypoint",
		"apply",
		"--namespace",
		nsConfig.Name(),
		"--service-account",
		"bookinfo-reviews",
	})
	waypointError := retry.UntilSuccess(func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), constants.GatewayNameLabel+"=bookinfo-waypoints")); err != nil {
			return fmt.Errorf("gateway is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(time.Minute), retry.BackoffDelay(time.Millisecond*100))
	if waypointError != nil {
		t.Fatal(waypointError)
	}
}

func setupBookinfo(t framework.TestContext, nsConfig namespace.Instance) {
	applyFileOrFail(t, nsConfig.Name(), bookinfoFile)
	bookinfoErr := retry.UntilSuccess(func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), "")); err != nil {
			return fmt.Errorf("bookinfo pods are not ready: %v", err)
		}
		return nil
	}, retry.Timeout(time.Minute*2), retry.BackoffDelay(time.Millisecond*500))
	if bookinfoErr != nil {
		t.Fatal(bookinfoErr)
	}
}

func skipsForTest(ctx resource.Context) {
	oldWorkloadClasses := ctx.Settings().SkipWorkloadClasses
	oldSkipVM, oldSkipTProxy := ctx.Settings().SkipVM, ctx.Settings().SkipTProxy
	ctx.Cleanup(func() {
		ctx.Settings().SkipWorkloadClasses = oldWorkloadClasses
		ctx.Settings().SkipVM = oldSkipVM
		ctx.Settings().SkipTProxy = oldSkipTProxy
	})
	ctx.Settings().SkipVM = true
	ctx.Settings().SkipTProxy = true
	ctx.Settings().SkipWorkloadClasses = append(ctx.Settings().SkipWorkloadClasses,
		echo.VM, echo.TProxy)
}

// applyFileOrFail applys the given yaml file and deletes it during context cleanup
func applyFileOrFail(t framework.TestContext, ns, filename string) {
	t.Helper()
	if err := t.ConfigIstio().File(ns, filename).Apply(apply.NoCleanup); err != nil {
		t.Fatal(err)
	}
}
