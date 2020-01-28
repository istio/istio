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

	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	kubeEnv "istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
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
	env                 environment.Name
	timeout             time.Duration
	namespace           namespace.Instance
	svcName             string
	clusterIP           string
	portName            string
	port                string
	san                 string
	expectedNumEndpoint int
	endpoints           []string
}

func newTestParams(ctx framework.TestContext, ns namespace.Instance) (t testParam) {
	ctx.Environment().Case(environment.Kube, func() {
		t = testParam{
			env:                 environment.Kube,
			timeout:             time.Second * 120,
			namespace:           ns,
			svcName:             "tcp-echo",
			portName:            "tcp",
			port:                "9000",
			san:                 "default",
			expectedNumEndpoint: 1,
		}
	})
	ctx.Environment().Case(environment.Native, func() {
		t = testParam{
			env:                 environment.Native,
			timeout:             time.Second * 60,
			namespace:           ns,
			svcName:             "kube-dns",
			clusterIP:           "10.43.240.10",
			portName:            "tcp-dns",
			port:                "53",
			san:                 "kube-dns",
			expectedNumEndpoint: 1,
			endpoints:           []string{"10.40.1.4"},
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
		testParamMu.Lock()
		defer testParamMu.Unlock()
		t.expectedNumEndpoint = 2
		if t.env == environment.Native {
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
		t.namespace.Name(),
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
	ctx := framework.NewContext(t)
	defer ctx.Done()

	ns := namespace.NewOrFail(t, ctx, namespace.Config{Prefix: "sse", Inject: true})
	testParams := newTestParams(ctx, ns)

	// apply a sse
	applyConfig(serviceEntry, testParams, t)

	collection := collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Name().String()

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

	steps := []struct {
		description string
		testCase    testcase
	}{
		{
			description: "check initial service and endpoint",
			testCase:    serviceEntry,
		},
		{
			description: "update service",
			testCase:    serviceUpdate,
		},
		{
			description: "update endpoint",
			testCase:    endpointUpdate,
		},
	}

	for _, s := range steps {
		switch s.testCase {
		case serviceEntry:
			// no op
		case serviceUpdate:
			applyConfig(serviceUpdate, testParams, t)
			testParams.update(serviceUpdate)
		case endpointUpdate:
			applyConfig(endpointUpdate, testParams, t)
			testParams.update(endpointUpdate)
		}
		t.Run(s.description, func(t *testing.T) {
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
					return true, nil
				})

			// need to verify endpoints with retry since env.(*kubeEnv.Environment).GetEndpoints
			// not always fetches endpoints immediately
			if err := validateEndpointsWithRetry(5, 10*time.Second, t, ctx, client, testParams); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func validateEndpointsWithRetry(try int, wait time.Duration, t *testing.T, ctx framework.TestContext, c echo.Instance, params testParam) (err error) {
	for i := 0; i <= try; i++ {
		err = verifyEndpoints(t, ctx, c, params)
		if err == nil {
			return
		}
		if i >= (try - 1) {
			break
		}
		time.Sleep(wait)
	}
	return fmt.Errorf("validateEndpointsWithRetry: try failed: %v", err)
}

func verifyEndpoints(t *testing.T, ctx framework.TestContext, c echo.Instance, params testParam) (err error) {
	endpoints := params.getEndpoints(ctx.Environment(), t)
	// wait for the new replica(s) to become ready
	if len(endpoints) != params.expectedNumEndpoint {
		return fmt.Errorf("verifyEndpoints: expected %d endpoints but only got %v", params.expectedNumEndpoint, endpoints)
	}
	workloads, _ := c.Workloads()
	for _, w := range workloads {
		if w.Sidecar() != nil {
			msg, err := w.Sidecar().Clusters()
			if err != nil {
				t.Fatal(err)
			}
			validator := structpath.ForProto(msg)
			for _, endpoint := range endpoints {
				err = validator.
					Select("{.clusterStatuses[?(@.name=='%v')]}", fmt.Sprintf("outbound|%s||%s.%s.svc.cluster.local", params.port, params.svcName, params.namespace.Name())).
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
	}
	return err
}

func applyConfig(testName testcase, params testParam, t *testing.T) {
	var config string

	initialDeployment := fmt.Sprintf("testdata/%s/initial_deployment.yaml", params.env)
	updateService := fmt.Sprintf("testdata/%s/update_service.yaml", params.env)
	updateEndpoint := fmt.Sprintf("testdata/%s/update_endpoints.yaml", params.env)

	switch testName {
	case serviceEntry:
		config = initialDeployment
	case serviceUpdate:
		g.DeleteConfigOrFail(t, params.namespace, file.AsStringOrFail(t, initialDeployment))
		config = updateService
	case endpointUpdate:
		g.DeleteConfigOrFail(t, params.namespace, file.AsStringOrFail(t, updateService))
		config = updateEndpoint
	}

	if err := g.ApplyConfigDir(params.namespace, config); err != nil {
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
	return galley.NewSingleObjectSnapshotValidator(params.namespace.Name(), func(ns string, actual *galley.SnapshotObject) error {
		sp := schema.MustGet().AllCollections().MustFind(collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Name().String())
		typeURL := "type.googleapis.com/" + sp.Resource().Proto()
		v := structpath.ForProto(actual)
		if err := v.Equals(typeURL, "{.TypeURL}").
			Equals(fmt.Sprintf("%s/%s", params.namespace.Name(), params.svcName), "{.Metadata.name}").
			Check(); err != nil {
			return err
		}
		// Compare the body
		inst := v.Select("{.Body}").
			Equals(fmt.Sprintf("%s.%s.svc.cluster.local", params.svcName, params.namespace.Name()), "{.hosts[0]}").
			Equals("MESH_INTERNAL", "{.location}").
			Equals("STATIC", "{.resolution}").
			Equals(fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", params.namespace.Name(), params.san), "{.subjectAltNames[0]}")
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
