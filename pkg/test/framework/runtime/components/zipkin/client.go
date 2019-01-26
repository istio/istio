//  Copyright 2018 Istio Authors
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

package zipkin

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

// Zipkin client is a shared client between the kube and native zipkin components.
type client struct {
	address string
}

func (c *client) GetAddress() string {
	return c.address
}

// Lists the services known to the zipkin server, returning an array of service names.
func (c *client) ListServices() (services []string, err error) {
	// Make a call to the services endpoint at /api/v1/services
	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/services", c.address))
	if err != nil {
		return
	}
	err = processResponse(resp, &services)
	if err != nil {
		return
	}

	return
}

// Returns a list of traces that match the given trace ID. These are traces that were created using
// the x-client-trace-id header.
func (c *client) GetTracesById(traceid string) (traces [][]map[string]interface{}, err error) {
	// Make a call to the traces endpoint at /api/v1/traces
	resp, err := http.Get(fmt.Sprintf(
		"http://%s/api/v1/traces?annotationQuery=guid:x-client-trace-id=%s",
		c.address, traceid))
	if err != nil {
		return
	}
	err = processResponse(resp, &traces)
	if err != nil {
		return
	}

	return
}

func processResponse(response *http.Response, v interface{}) error {
	defer func() {
		_ = response.Body.Close()
	}()

	bytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		err = fmt.Errorf("Error contacting zipkin server, got %s:\n%s", response.StatusCode, string(bytes))
		return err
	}
	return json.Unmarshal(bytes, v)
}
