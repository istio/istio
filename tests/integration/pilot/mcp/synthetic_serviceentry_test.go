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

package mcp

import (
	"fmt"
	"sync"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	kubeEnv "istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/structpath"
)

type testcase string

const (
	serviceEntry   testcase = "serviceEntry"
	serviceUpdate  testcase = "serviceEntry_service_update"
	endpointUpdate testcase = "serviceEntry_endpoint_update"
)

var testParamMu = &sync.Mutex{}

type testParam struct {
	env       environment.Name
	timeout   time.Duration
	namespace string
	svcName   string
	clusterIP string
	portName  string
	port      string
	san       string
	endpoints []string
}

func newTestParams(ctx framework.TestContext, ns string) (t testParam) {
	ctx.Environment().Case(environment.Kube, func() {
		t = testParam{
			env:       environment.Kube,
			timeout:   time.Second * 240,
			namespace: ns,
			svcName:   "tcp-echo",
			portName:  "tcp",
			port:      "9000",
			san:       "default",
		}
	})
	ctx.Environment().Case(environment.Native, func() {
		t = testParam{
			env:       environment.Native,
			timeout:   time.Second * 60,
			namespace: ns,
			svcName:   "kube-dns",
			clusterIP: "10.43.240.10",
			portName:  "tcp-dns",
			port:      "53",
			san:       "kube-dns",
			endpoints: []string{"10.40.1.4"},
		}
	})
	return t
}

func (t *testParam) update(test testcase) {
	switch test {
	case serviceUpdate:
		if t.env == environment.Native {
			t.clusterIP = "10.43.240.11"
			return
		}
		t.port = "9001"
	case endpointUpdate:
		if t.env == environment.Native {
			testParamMu.Lock()
			defer testParamMu.Unlock()
			t.endpoints = append(t.endpoints, "10.40.1.5")
			return
		}
	}
}

func (t *testParam) getEndpoints(env resource.Environment, tt *testing.T) (addresses []string) {

	if env.EnvironmentName() == environment.Native {
		testParamMu.Lock()
		defer testParamMu.Unlock()
		return t.endpoints
	}

	eps, err := env.(*kubeEnv.Environment).GetEndpoints(
		t.namespace,
		t.svcName,
		kubeApiMeta.GetOptions{})

	if err != nil {
		tt.Fatal(err)
	}
	if len(eps.Subsets) > 0 {
		for _, addr := range eps.Subsets[0].Addresses {
			addresses = append(addresses, addr.IP)
		}
	}
	return addresses
}

func TestSyntheticServiceEntry(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/17953")
	ctx := framework.NewContext(t)
	defer ctx.Done()

	ns := namespace.NewOrFail(t, ctx, namespace.Config{Prefix: "sse", Inject: true})
	testParams := newTestParams(ctx, ns.Name())

	// apply a sse
	applyConfig(serviceEntry, ns, ctx, t)

	collection := metadata.IstioNetworkingV1alpha3SyntheticServiceentries.Collection.String()

	if err := g.WaitForSnapshot(collection, syntheticServiceEntryValidator(testParams)); err != nil {
		t.Fatalf("failed waiting for %s:\n%v\n", collection, err)
	}

	var client echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&client, echo.Config{
			Service:   "echo",
			Namespace: ns,
			Pilot:     p,
			Galley:    g,
		}).BuildOrFail(t)

	nodeID := client.WorkloadsOrFail(t)[0].Sidecar().NodeID()
	discoveryReq := pilot.NewDiscoveryRequest(nodeID, pilot.Listener)
	p.StartDiscoveryOrFail(t, discoveryReq)

	t.Run("check initial service and endpoint", func(t *testing.T) {
		p.WatchDiscoveryOrFail(t, testParams.timeout,
			func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
				validator := structpath.ForProto(response)
				instance := validator.Select("{.resources[?(@.address.socketAddress.portValue==%v)]}", testParams.port)
				if instance.Check() != nil {
					return false, nil
				}
				if err := validateSse(validator, testParams); err != nil {
					return false, nil
				}
				endpoint := testParams.getEndpoints(ctx.Environment(), t)
				// wait for the new instance to become ready
				if len(endpoint) == 0 {
					return false, nil
				}
				if err := verifyEndpoints(t, client, testParams, endpoint[0]); err != nil {
					return false, nil
				}
				return true, nil
			})

	})

	//service update
	applyConfig(serviceUpdate, ns, ctx, t)
	testParams.update(serviceUpdate)

	t.Run("update service", func(t *testing.T) {
		p.WatchDiscoveryOrFail(t, testParams.timeout,
			func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
				validator := structpath.ForProto(response)
				if validator.Select("{.resources[?(@.address.socketAddress.portValue==%v)]}", testParams.port).Check() != nil {
					return false, nil
				}
				if err := validateSse(validator, testParams); err != nil {
					return false, nil
				}
				endpoint := testParams.getEndpoints(ctx.Environment(), t)
				// wait for the new instance to become ready
				if len(endpoint) == 0 {
					return false, nil
				}
				if err := verifyEndpoints(t, client, testParams, endpoint[0]); err != nil {
					return false, nil
				}
				return true, nil
			})
	})

	// endpoint update
	applyConfig(endpointUpdate, ns, ctx, t)
	testParams.update(endpointUpdate)

	t.Run("update endpoint", func(t *testing.T) {
		p.WatchDiscoveryOrFail(t, testParams.timeout,
			func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
				validator := structpath.ForProto(response)
				if validator.Select("{.resources[?(@.address.socketAddress.portValue==%v)]}", testParams.port).Check() != nil {
					return false, nil
				}
				if err := validateSse(validator, testParams); err != nil {
					return false, nil
				}
				endpoints := testParams.getEndpoints(ctx.Environment(), t)
				// wait for the new replica to become ready
				if len(endpoints) != 2 {
					return false, nil
				}
				// verify endpoints
				for _, e := range endpoints {
					if err := verifyEndpoints(t, client, testParams, e); err != nil {
						fmt.Println(err)
						return false, nil
					}

				}
				return true, nil
			})
	})
}

func verifyEndpoints(t *testing.T, c echo.Instance, params testParam, endpoint string) (err error) {
	workloads, _ := c.Workloads()
	for _, w := range workloads {
		if w.Sidecar() != nil {
			msg, err := w.Sidecar().Clusters()
			if err != nil {
				t.Fatal(err)
			}
			validator := structpath.ForProto(msg)
			err = validator.
				Select("{.clusterStatuses[?(@.name=='%v')]}", fmt.Sprintf("outbound|%s||%s.%s.svc.cluster.local", params.port, params.svcName, params.namespace)).
				Equals("true", "{.addedViaApi}").
				ContainSubstring(endpoint, "{.hostStatuses}").
				ContainSubstring(params.port, "{.hostStatuses}").
				ContainSubstring("HEALTHY", "{.hostStatuses}").
				Check()
			if err != nil {
				return err
			}
		}
	}
	return err
}

func applyConfig(testName testcase, namespace namespace.Instance, ctx framework.TestContext, t *testing.T) {
	var folder, config string
	//TODO (Nino-K): investigate why the clearConfig can not be triggered either during
	// service update or endpoint update for each env
	// Tringgering clear Config at the same time (e.g during ServiceUpdate or EndpointUpdate)
	// for each env makes galley not to always forward the endpoints along with the SE which
	// can make the test flaky
	ctx.Environment().Case(environment.Kube, func() {
		if testName == serviceUpdate {
			if err := g.ClearConfig(); err != nil {
				t.Fatal(err)
			}
		}
		folder = "kube"
	})
	ctx.Environment().Case(environment.Native, func() {
		if testName == endpointUpdate {
			if err := g.ClearConfig(); err != nil {
				t.Fatal(err)
			}
		}

		folder = "native"
	})

	switch testName {
	case serviceEntry:
		config = fmt.Sprintf("testdata/%s/initial_deployment.yaml", folder)
	case serviceUpdate:
		config = fmt.Sprintf("testdata/%s/update_service.yaml", folder)
	case endpointUpdate:
		config = fmt.Sprintf("testdata/%s/update_endpoints.yaml", folder)
	}

	if err := g.ApplyConfigDir(namespace, config); err != nil {
		t.Fatal(err)
	}
}

func validateSse(response *structpath.Instance, params testParam) error {
	return response.
		Select("{.resources[?(@.address.socketAddress.portValue==%s)]}", params.port).
		ContainSubstring(params.port, "{.name}").
		Equals(params.port, "{.address.socketAddress.portValue}").
		ContainSubstring(fmt.Sprintf("outbound|%s||%s", params.port, params.svcName), "{.filterChains[0]}").
		Check()
}

func syntheticServiceEntryValidator(params testParam) galley.SnapshotValidatorFunc {
	return galley.NewSingleObjectSnapshotValidator(params.namespace, func(ns string, actual *galley.SnapshotObject) error {
		v := structpath.ForProto(actual)
		if err := v.Equals(metadata.IstioNetworkingV1alpha3SyntheticServiceentries.TypeURL.String(), "{.TypeURL}").
			Equals(fmt.Sprintf("%s/%s", params.namespace, params.svcName), "{.Metadata.name}").
			Check(); err != nil {
			return err
		}
		// Compare the body
		inst := v.Select("{.Body}").
			Equals(fmt.Sprintf("%s.%s.svc.cluster.local", params.svcName, params.namespace), "{.hosts[0]}").
			Equals(1, "{.location}").
			Equals(1, "{.resolution}").
			Equals(fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", params.namespace, params.san), "{.subject_alt_names[0]}")
		if params.env == environment.Native {
			inst.Equals(params.clusterIP, "{.addresses[0]}")
		}
		if err := inst.Check(); err != nil {
			return err
		}
		// Compare Port
		if err := v.Select("{.Body.ports[0]}").
			Equals(params.portName, "{.name}").
			Equals(params.port, "{.number}").
			Equals("TCP", "{.protocol}").
			Check(); err != nil {
			return err
		}
		return nil
	})
}
