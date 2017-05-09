// Copyright 2017 Istio Authors
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

// API server tests

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	net_http "net/http"
	"reflect"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/manager/apiserver"
	"istio.io/manager/cmd"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
)

type apiServerTest struct {
	*infra

	stopChannel chan struct{}
	server      *apiserver.API
}

type httpRequest struct {
	method               string
	url                  string
	data                 interface{}
	expectedResponseCode int
	expectedBody         interface{}

	// These HTTP methods are not correct, but allowed before the rules database becomes consistent
	retryOn map[int]bool

	// These response values are not correct, but allowed before the rules database becomes consistent
	retryIfMatch []interface{}
}

var (
	jsonRule = map[string]interface{}{
		"type": "route-rule",
		"name": "reviews-default",
		"spec": map[string]interface{}{
			"destination": "reviews.default.svc.cluster.local",
			"precedence":  float64(1),
			"route": []interface{}{map[string]interface{}{
				"tags": map[string]interface{}{
					"version": "v1",
				},
				"weight": float64(100),
			}},
		},
	}

	jsonRule2 = map[string]interface{}{
		"type": "route-rule",
		"name": "reviews-default",
		"spec": map[string]interface{}{
			"destination": "reviews.default.svc.cluster.local",
			"precedence":  float64(1),
			"route": []interface{}{map[string]interface{}{
				"tags": map[string]interface{}{
					"version": "v2",
				},
				"weight": float64(100),
			}},
		},
	}

	jsonInvalidRule = map[string]interface{}{
		"type": "route-rule",
		"name": "reviews-invalid",
		"spec": map[string]interface{}{
			"destination": "reviews.default.svc.cluster.local",
			"precedence":  float64(1),
			"route": []interface{}{map[string]interface{}{
				"tags": map[string]interface{}{
					"version": "v1",
				},
				"weight": float64(999),
			}},
		},
	}

	// The URL we will use for talking directly to apiserver about the test rule
	testURL string

	// The URL of all the apiserver rules in the test namespace
	testNamespaceURL string
)

func (r *apiServerTest) String() string {
	return "apiserver"
}

func (r *apiServerTest) setup() error {

	// Start apiserver outside the cluster.

	var err error
	// receive mesh configuration
	mesh, err := cmd.GetMeshConfig(istioClient.GetKubernetesClient(), r.Namespace, "istio")
	if err != nil {
		return multierror.Append(err, fmt.Errorf("failed to retrieve mesh configuration"))
	}

	controllerOptions := kube.ControllerOptions{}
	controller := kube.NewController(istioClient, mesh, controllerOptions)
	r.server = apiserver.NewAPI(apiserver.APIServiceOptions{
		Version:  kube.IstioResourceVersion,
		Port:     8081,
		Registry: &model.IstioRegistry{ConfigRegistry: controller},
	})
	r.stopChannel = make(chan struct{})
	go controller.Run(r.stopChannel)
	go r.server.Run()

	// Wait until apiserver is ready.  (As far as I can see there is no ready channel)
	for i := 0; i < 10; i++ {
		_, err := net_http.Get("http://localhost:8081/")
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	testURL = "http://localhost:8081/v1alpha1/config/route-rule/" + r.Namespace + "/reviews-default"
	testNamespaceURL = "http://localhost:8081/v1alpha1/config/route-rule/" + r.Namespace

	return nil
}

func (r *apiServerTest) teardown() {
	if r.stopChannel != nil {
		close(r.stopChannel)
	}

	if r.server != nil {
		r.server.Shutdown(context.Background())
	}
}

func (r *apiServerTest) run() error {
	if err := r.routeRuleInvalidDetected(); err != nil {
		return err
	}
	if err := r.routeRuleCRUD(); err != nil {
		return err
	}
	return nil
}

func (r *apiServerTest) routeRuleInvalidDetected() error {
	httpSequence := []httpRequest{
		// Can't create invalid rules
		{
			method: "POST", url: "http://localhost:8081/v1alpha1/config/route-rule/" + r.Namespace + "/reviews-invalid",
			data:                 jsonInvalidRule,
			expectedResponseCode: net_http.StatusInternalServerError,
		},
	}

	return verifySequence("invalid rules", httpSequence)
}

// routeRuleCRUD attempts to talk to the apiserver to Create, Retrieve, Update, and Delete a rule
func (r *apiServerTest) routeRuleCRUD() error {

	// Run through the lifecycle of updating a rule, with errors, to verify the sequence works as expected.
	httpSequence := []httpRequest{
		// Step 0 Can't get before created
		{
			method: "GET", url: testURL,
			expectedResponseCode: net_http.StatusNotFound,
		},
		// Step 1 Can create
		{
			method: "POST", url: testURL,
			data:                 jsonRule,
			expectedResponseCode: net_http.StatusCreated,
			expectedBody:         jsonRule,
			retryOn:              map[int]bool{net_http.StatusInternalServerError: true},
		},
		// Step 2 Can't create twice
		{
			// TODO should be StatusForbidden but the server is returning 500 (incorrectly?)
			method: "POST", url: testURL,
			data:                 jsonRule,
			expectedResponseCode: net_http.StatusInternalServerError,
		},
		//  Appears in the namespace after creation
		{
			method: "GET", url: testNamespaceURL,
			expectedResponseCode: net_http.StatusOK,
			expectedBody:         []interface{}{jsonRule},
			retryOn:              map[int]bool{net_http.StatusNotFound: true},
			retryIfMatch:         []interface{}{[]interface{}{}},
		},
		// Step 3 Can get
		{
			method: "GET", url: testURL,
			expectedResponseCode: net_http.StatusOK,
			expectedBody:         jsonRule,
			retryOn:              map[int]bool{net_http.StatusNotFound: true},
		},
		// Step 4: Can update
		{
			method: "PUT", url: testURL,
			data:                 jsonRule2,
			expectedResponseCode: net_http.StatusOK,
			expectedBody:         jsonRule2,
		},
		// Can still GET after update
		{
			method: "GET", url: testURL,
			expectedResponseCode: net_http.StatusOK,
			expectedBody:         jsonRule2,
			retryOn:              map[int]bool{net_http.StatusNotFound: true},
			retryIfMatch:         []interface{}{jsonRule},
		},
		// Can delete
		{
			method: "DELETE", url: testURL,
			expectedResponseCode: net_http.StatusOK, // Should (perhaps?) be StatusNoContent
			retryOn:              map[int]bool{net_http.StatusNotFound: true},
		},
		// Cannot retrieve after delete
		{
			method: "GET", url: testURL,
			expectedResponseCode: net_http.StatusNotFound,
			retryOn:              map[int]bool{net_http.StatusOK: true},
		},
		// Can't delete twice
		{
			method: "DELETE", url: testURL,
			expectedResponseCode: net_http.StatusInternalServerError, // Should be StatusNotFound
		},
	}

	err := verifySequence("CRUD", httpSequence)

	// On error, clean up any rule we created that didn't get deleted by the final requests (in case count>1)
	if err != nil {
		// Attempt to delete rule, in case we created this and left it around due to a failure.
		client := &net_http.Client{}
		if req, err2 := net_http.NewRequest("DELETE", testURL, nil); err2 != nil {
			glog.V(2).Infof("Could not create request to clean up")
		} else {
			if _, err2 := client.Do(req); err2 != nil {
				glog.V(2).Infof("Could not DELETE possible leftover Istio rule")
			}
		}
	}

	return err
}

// Verify a sequence of HTTP requests produce the expected responses
func verifySequence(name string, httpSequence []httpRequest) error {

	for rulenum, hreq := range httpSequence {

		var err error
		var bytesToSend []byte
		if hreq.data != nil {
			bytesToSend, err = json.Marshal(hreq.data)
			if err != nil {
				return err
			}
		}

		glog.V(2).Infof("Starting %v step %v", name, rulenum)

		// Verify a correct response comes back.  Retry up to 5 times in case data is initially incorrect
		if err = verifyRequest(hreq, bytesToSend, 5, rulenum); err != nil {
			return err
		}
	}

	return nil
}

// Verify an HTTP requests produces the expected responses or a small number of the expected error responses
func verifyRequest(hreq httpRequest, bytesToSend []byte, maxRetries int, rulenum int) (problem error) {
	client := &net_http.Client{}

	// Keep track if we need to retry before apiserver becomes consistent
	retries := 0
	retryable := true
	for retryable {
		retryable = false
		glog.V(2).Infof("Creating Request for %v %v\n", hreq.method, hreq.url)

		req, err := net_http.NewRequest(hreq.method, hreq.url, bytes.NewBuffer(bytesToSend))
		if err != nil {
			return err
		}

		if hreq.method == "POST" || hreq.method == "PUT" {
			req.Header.Add("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if err = resp.Body.Close(); err != nil {
			return err
		}

		// Did apiserver fail to return the expected response code?
		if resp.StatusCode != hreq.expectedResponseCode {
			glog.V(2).Infof("Request for %v %v got status code %v\n", hreq.method, hreq.url, resp.StatusCode)

			problem = fmt.Errorf("%v to %q expected %v but got %v %q on step %v after %v attempts", hreq.method, hreq.url,
				hreq.expectedResponseCode, resp.StatusCode, body, rulenum, retries+1)

			// Did it return a code that could lead to a correct code after data becomes consistent?
			if rt, ok := hreq.retryOn[resp.StatusCode]; !ok || !rt || retries >= maxRetries {
				break
			}

			// Give the system a moment after data-changing operations
			time.Sleep(1 * time.Second)
			glog.V(2).Infof("retrying %v %v because of status code\n", hreq.method, hreq.url)
			retries++
			retryable = true
			continue
		}

		// Response code matches expectation.  We are done if we don't require a particular response body
		if hreq.expectedBody == nil {
			return nil
		}

		var jsonBody interface{}
		if err = json.Unmarshal(body, &jsonBody); err == nil {
			if reflect.DeepEqual(jsonBody, hreq.expectedBody) {
				return nil
			}

			glog.V(2).Infof("Request for %v %v got expected status code but body did not match: %s\n",
				hreq.method, hreq.url, body)

			serializedExpectedBody, err := json.Marshal(hreq.expectedBody)
			if err != nil {
				return err
			}
			problem = fmt.Errorf("%v to %q expected JSON body %v but got %v on step %v after %v attempts", hreq.method, hreq.url,
				string(serializedExpectedBody), string(body), rulenum, retries+1)

			if retries >= maxRetries {
				break
			}

			// Does the data match transient data we tolerate while waiting for consistency?
			for _, retryIfMatch := range hreq.retryIfMatch {
				if reflect.DeepEqual(jsonBody, retryIfMatch) {
					// Give the system a moment after data-changing operations
					time.Sleep(1 * time.Second)
					retries++
					retryable = true
					glog.V(2).Infof("Retrying %v %v because of body\n", hreq.method, hreq.url)
				} else {
					glog.V(2).Infof("%v didn't match %v\n", jsonBody, retryIfMatch)
				}
			}
		} else {
			problem = fmt.Errorf("%v to %q expected body %v but got %q on step %v after %v attempts", hreq.method, hreq.url,
				hreq.expectedBody, body, rulenum, retries+1)
			// The returned data was not JSON
			if retries >= maxRetries {
				break
			}

			retries++
			retryable = true
		}

	} // end for (ever)

	return
}
