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

package apiserver

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"istio.io/pilot/model"
)

// Config is the complete configuration including a parsed spec
type Config struct {
	// Type SHOULD be one of the kinds in model.IstioConfig; a route-rule, ingress-rule, or destination-policy
	Type string      `json:"type,omitempty"`
	Name string      `json:"name,omitempty"`
	Spec interface{} `json:"spec,omitempty"`
	// ParsedSpec will be one of the messages in model.IstioConfig: for example an
	// istio.proxy.v1alpha.config.RouteRule or DestinationPolicy
	ParsedSpec proto.Message `json:"-"`
}

// ParseSpec takes the field in the config object and parses into a protobuf message
// Then assigns it to the ParseSpec field
func (c *Config) ParseSpec() error {

	byteSpec, err := json.Marshal(c.Spec)
	if err != nil {
		return fmt.Errorf("could not encode Spec: %v", err)
	}
	schema, ok := model.IstioConfig[c.Type]
	if !ok {
		return fmt.Errorf("unknown spec type %s", c.Type)
	}
	message, err := schema.FromJSON(string(byteSpec))
	if err != nil {
		return fmt.Errorf("cannot parse proto message: %v", err)
	}
	c.ParsedSpec = message
	glog.V(2).Infof("Parsed %v %v into %v %v", c.Type, c.Name, schema.MessageName, message)
	return nil
}
