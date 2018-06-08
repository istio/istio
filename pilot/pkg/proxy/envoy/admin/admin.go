// Copyright 2018 Istio Authors
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

package admin

import (
	"fmt"
	"io"
	"net/http"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
)

// API is a Stdout/Stderr wrapper around the Envoy Admin API that converts the proto to JSON
type API struct {
	URL            string
	Stdout, Stderr io.Writer
}

const (
	clustersKey  = "clusters"
	listenersKey = "listeners"
	routesKey    = "routes"
	bootstrapKey = "bootstrap"
)

// PrintClusterDump prints just the cluster config dump to the API stdout
func (a *API) PrintClusterDump() {
	a.genericPrinter(clustersKey)
}

// PrintListenerDump prints just the listener config dump to the API stdout
func (a *API) PrintListenerDump() {
	a.genericPrinter(listenersKey)
}

// PrintRoutesDump prints just the routes config dump to the API stdout
func (a *API) PrintRoutesDump() {
	a.genericPrinter(routesKey)
}

// PrintBootstrapDump prints just the bootstrap config dump to the API stdout
func (a *API) PrintBootstrapDump() {
	a.genericPrinter(bootstrapKey)
}

func (a *API) genericPrinter(configKey string) {
	configDump, err := a.retrieveConfigDump()
	if err != nil {
		fmt.Fprintf(a.Stderr, err.Error())
		return
	}
	scopedDump, ok := configDump.Configs[configKey]
	if !ok {
		fmt.Fprintf(a.Stderr, "unable to find %v in Envoy config dump", configKey)
		return
	}
	jsonm := &jsonpb.Marshaler{Indent: "    "}
	if err := jsonm.Marshal(a.Stdout, &scopedDump); err != nil {
		fmt.Fprintf(a.Stderr, "unable to marshal %v in Envoy config dump", configKey)
	}
}

func (a *API) retrieveConfigDump() (*adminapi.ConfigDump, error) {
	fullURL := fmt.Sprintf("%v/config_dump", a.URL)
	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("error retrieving config dump from Envoy: %v", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("received %v status retrieving config dump from Envoy", resp.StatusCode)
	}
	defer resp.Body.Close()
	configDump := adminapi.ConfigDump{}
	jsonum := &jsonpb.Unmarshaler{}
	err = jsonum.Unmarshal(resp.Body, &configDump)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling config dump response from Envoy: %v", err)
	}
	return &configDump, nil
}
