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

package configdump

import (
	"fmt"
	"io"
	"net/http"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
)

// JSONPrinter is a Stdout/Stderr wrapper around the Envoy Admin config_dump endpoint that converts the proto to JSON
type JSONPrinter struct {
	URL            string
	Stdout, Stderr io.Writer
}

const (
	clustersKey  = "clusters"
	listenersKey = "listeners"
	routesKey    = "routes"
	bootstrapKey = "bootstrap"
)

// PrintClusterDump prints just the cluster config dump to the JSONPrinter stdout
func (p *JSONPrinter) PrintClusterDump() {
	p.genericPrinter(clustersKey)
}

// PrintListenerDump prints just the listener config dump to the JSONPrinter stdout
func (p *JSONPrinter) PrintListenerDump() {
	p.genericPrinter(listenersKey)
}

// PrintRoutesDump prints just the routes config dump to the JSONPrinter stdout
func (p *JSONPrinter) PrintRoutesDump() {
	p.genericPrinter(routesKey)
}

// PrintBootstrapDump prints just the bootstrap config dump to the JSONPrinter stdout
func (p *JSONPrinter) PrintBootstrapDump() {
	p.genericPrinter(bootstrapKey)
}

func (p *JSONPrinter) genericPrinter(configKey string) {
	configDump, err := p.retrieveConfigDump()
	if err != nil {
		fmt.Fprintf(p.Stderr, err.Error())
		return
	}
	scopedDump, ok := configDump.Configs[configKey]
	if !ok {
		fmt.Fprintf(p.Stderr, "unable to find %v in Envoy config dump", configKey)
		return
	}
	jsonm := &jsonpb.Marshaler{Indent: "    "}
	if err := jsonm.Marshal(p.Stdout, &scopedDump); err != nil {
		fmt.Fprintf(p.Stderr, "unable to marshal %v in Envoy config dump", configKey)
	}
}

func (p *JSONPrinter) retrieveConfigDump() (*adminapi.ConfigDump, error) {
	fullURL := fmt.Sprintf("%v/config_dump", p.URL)
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
