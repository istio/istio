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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	ambientComponent "istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/assert"
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
	templateFile    = "manifests/charts/istio-control/istio-discovery/files/waypoint.yaml"
)

func TestBookinfo(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			skipsForTest(t)
			nsConfig, err := namespace.New(t, namespace.Config{
				Prefix: "book",
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
			addrs, ingrPorts := ingressInst.HTTPAddresses()
			var ingressURLs []string
			for i, addr := range addrs {
				ingressURLs = append(ingressURLs, fmt.Sprintf("http://%v:%v", addr, ingrPorts[i]))
			}
			t.NewSubTest("no waypoint").Run(func(t framework.TestContext) {
				t.NewSubTest("productpage reachable").Run(func(t framework.TestContext) {
					for _, ingressURL := range ingressURLs {
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
					}
				})
			})

			ambientComponent.NewWaypointProxyOrFail(t, nsConfig, "namespace")

			t.NewSubTest("ingress receives waypoint updates").Run(func(t framework.TestContext) {
				ambientComponent.AddWaypointToService(t, nsConfig, "productpage", "namespace")
				for _, ingressURL := range ingressURLs {
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := ingressClient.Get(ingressURL + "/productpage")
						if err != nil {
							return fmt.Errorf("error fetching /productpage: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}
						return nil
					}, retry.Converge(5))
				}
				ambientComponent.RemoveWaypointFromService(t, nsConfig, "productpage", "namespace")
				for _, ingressURL := range ingressURLs {
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := ingressClient.Get(ingressURL + "/productpage")
						if err != nil {
							return fmt.Errorf("error fetching /productpage: %v", err)
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							return fmt.Errorf("expect status code %v, got %v", http.StatusFound, resp.StatusCode)
						}
						return nil
					}, retry.Converge(5))
				}
			})

			t.NewSubTest("waypoint routing").Run(func(t framework.TestContext) {
				t.NewSubTest("productpage reachable").Run(func(t framework.TestContext) {
					for _, ingressURL := range ingressURLs {
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
					}
				})

				ambientComponent.AddWaypointToService(t, nsConfig, "reviews", "namespace")

				t.NewSubTest("reviews v1").Run(func(t framework.TestContext) {
					applyFileOrFail(t, nsConfig.Name(), routingV1)
					for _, ingressURL := range ingressURLs {
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
					}
				})

				t.NewSubTest("reviews v2").Run(func(t framework.TestContext) {
					applyFileOrFail(t, nsConfig.Name(), headerRouting)
					cookieClient := http.Client{
						Jar: jar,
						CheckRedirect: func(req *http.Request, via []*http.Request) error {
							return http.ErrUseLastResponse
						},
					}
					for _, ingressURL := range ingressURLs {
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
					}
				})
			})
			t.NewSubTest("waypoint template change").Run(func(t framework.TestContext) {
				// Test will modify grace period as an arbitrary change to check we re-deploy the waypoint
				getGracePeriod := func(want int64) bool {
					pods, err := kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), constants.GatewayNameLabel+"=namespace")()
					assert.NoError(t, err)
					for _, p := range pods {
						grace := p.Spec.TerminationGracePeriodSeconds
						if grace != nil && *grace == want {
							return true
						}
					}
					return false
				}
				// check that waypoint deployment is unmodified
				retry.UntilOrFail(t, func() bool {
					return getGracePeriod(2)
				})
				// modify template
				istio.GetOrFail(t, t).UpdateInjectionConfig(t, func(c *inject.Config) error {
					mainTemplate := file.MustAsString(filepath.Join(env.IstioSrc, templateFile))
					c.RawTemplates["waypoint"] = strings.ReplaceAll(mainTemplate, "terminationGracePeriodSeconds: 2", "terminationGracePeriodSeconds: 3")
					return nil
				}, cleanup.Always)
				// check that waypoint deployment is modified
				retry.UntilOrFail(t, func() bool {
					return getGracePeriod(3)
				})
			})
			t.CleanupConditionally(func() {
				ambientComponent.RemoveWaypointFromService(t, nsConfig, "reviews", "namespace")
				ambientComponent.DeleteWaypoint(t, nsConfig, "namespace")
			})
		})
}

func TestOtherRevisionIgnored(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			// This is a negative test, ensuring gateways with tags other
			// than my tags do not get controlled by me.
			nsConfig, err := namespace.New(t, namespace.Config{
				Prefix: "badgateway",
				Inject: false,
				Labels: map[string]string{
					constants.DataplaneMode: "ambient",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"apply",
				"--namespace",
				nsConfig.Name(),
				"--service-account",
				"sa",
				"--revision",
				"foo",
			})
			waypointError := retry.UntilSuccess(func() error {
				fetch := kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), constants.GatewayNameLabel+"="+"sa")
				if _, err := kubetest.CheckPodsAreReady(fetch); err != nil {
					return fmt.Errorf("gateway is not ready: %v", err)
				}
				return nil
			}, retry.Timeout(15*time.Second), retry.BackoffDelay(time.Millisecond*100))
			if waypointError == nil {
				t.Fatal("Waypoint for non-existent tag foo created deployment!")
			}
		})
}

func applyDefaultRouting(t framework.TestContext, nsConfig namespace.Instance) {
	applyFileOrFail(t, nsConfig.Name(), defaultDestRule)
	applyFileOrFail(t, nsConfig.Name(), bookinfoGateway)
	applyFileOrFail(t, nsConfig.Name(), routingV1)
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
