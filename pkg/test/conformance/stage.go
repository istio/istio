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

package conformance

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/test/conformance/constraint"
)

const (
	// InputFileName is the canonical name for the resource file.
	InputFileName = "input.yaml"

	// MeshConfigFileName is the name of the meshconfig file.
	MeshConfigFileName = "meshconfig.yaml"

	// MCPFileName is the name of the MCP constraint file.
	MCPFileName = "mcp.yaml"

	// TrafficFileName is the name of the traffic test/constrains file.
	TrafficFileName = "traffic.yaml"
)

type Traffic struct {
	Services []Service
	Calls    []Call
}

type Port struct {
	Name        string
	Protocol    string // protocol.Instance
	ServicePort uint16
}

type Service struct {
	Name  string
	Ports []Port
}

type Response struct {
	Code   int
	Callee string
}

type Call struct {
	Caller   string
	URL      string
	Response Response
}

// Stage of a test.
type Stage struct {
	// Input resources (in Yaml format)
	Input string

	// MCP validation rules
	MCP *constraint.Constraints

	// MeshConfig (if any) to apply for this stage.
	MeshConfig *string

	// Traffic definition and constraints for this stage.
	Traffic *Traffic
}

func hasStages(dir string) (bool, error) {
	entries, err := ioutil.ReadDir(filepath.Join(dir))
	if err != nil {
		return false, err
	}

	for _, e := range entries {
		if e.Name() == "stage0" {
			return true, nil
		}
	}

	return false, nil
}

func loadStage(dir string) (*Stage, error) {
	input, err := ioutil.ReadFile(filepath.Join(dir, InputFileName))
	if err != nil {
		return nil, fmt.Errorf("error loading input file: %v", err)
	}

	var meshconfig *string
	meshconfigBytes, err := ioutil.ReadFile(filepath.Join(dir, MeshConfigFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		meshconfigBytes = nil
	}
	if meshconfigBytes != nil {
		s := string(meshconfigBytes)
		meshconfig = &s
	}

	var mcp *constraint.Constraints
	mcpBytes, err := ioutil.ReadFile(filepath.Join(dir, MCPFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		mcpBytes = nil
	}
	if mcpBytes != nil {
		if mcp, err = constraint.Parse(mcpBytes); err != nil {
			return nil, fmt.Errorf("MCP parse error: %v", err)
		}
	}

	var traffic *Traffic
	switch bs, err := ioutil.ReadFile(filepath.Join(dir, TrafficFileName)); {
	case err == nil:
		traffic = new(Traffic)
		if err := yaml.UnmarshalStrict(bs, traffic); err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
	default:
		return nil, err
	}

	return &Stage{
		Input:      string(input),
		MeshConfig: meshconfig,
		MCP:        mcp,
		Traffic:    traffic,
	}, nil
}

func parseStageName(s string) (int, bool) {
	if !strings.HasPrefix(s, "stage") {
		return -1, false
	}

	s = s[len("stage"):]
	i, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return -2, false
	}

	return int(i), true
}
