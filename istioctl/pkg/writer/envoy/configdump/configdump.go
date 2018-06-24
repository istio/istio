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
	"bytes"
	"fmt"
	"io"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

// ConfigWriter is a writer for processing responses from the Envoy Admin config_dump endpoint
type ConfigWriter struct {
	Stdout     io.Writer
	configDump *adminapi.ConfigDump
}

const (
	clustersKey  = "clusters"
	listenersKey = "listeners"
	routesKey    = "routes"
	bootstrapKey = "bootstrap"
)

// Prime loads the config dump into the writer ready for printing
func (c *ConfigWriter) Prime(b []byte) error {
	buffer := bytes.NewBuffer(b)
	c.configDump = &adminapi.ConfigDump{}
	jsonum := &jsonpb.Unmarshaler{}
	err := jsonum.Unmarshal(buffer, c.configDump)
	if err != nil {
		return fmt.Errorf("error unmarshalling config dump response from Envoy: %v", err)
	}
	return nil
}

// PrintBootstrapDump prints just the bootstrap config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintBootstrapDump() error {
	return c.genericPrinter(bootstrapKey)
}

func (c *ConfigWriter) genericPrinter(configKey string) error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	scopedDump, ok := c.configDump.Configs[configKey]
	if !ok {
		return fmt.Errorf("unable to find %v in Envoy config dump", configKey)
	}
	jsonm := &jsonpb.Marshaler{Indent: "    "}
	if err := jsonm.Marshal(c.Stdout, &scopedDump); err != nil {
		return fmt.Errorf("unable to marshal %v in Envoy config dump", configKey)
	}
	return nil
}

// protoMessageSlice allows us to marshal slices of protobuf messages like clusters/listeners/routes correctly
type protoMessageSlice []interface{}

func (pSLice protoMessageSlice) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString("[")
	sliceLength := len(pSLice)
	jsonm := &jsonpb.Marshaler{}
	for index, msg := range pSLice {
		p, ok := msg.(proto.Message)
		if !ok {
			return nil, fmt.Errorf("object at index %v doesn't statisfy proto.Message interface", index)
		}
		if err := jsonm.Marshal(buffer, p); err != nil {
			return nil, err
		}
		if index < sliceLength-1 {
			buffer.WriteString(",")
		}
	}
	buffer.WriteString("]")
	return buffer.Bytes(), nil
}
