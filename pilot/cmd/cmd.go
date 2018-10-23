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

package cmd

import (
	"io/ioutil"

	multierror "github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

// ReadMeshConfig gets mesh configuration from a config file
func ReadMeshConfig(filename string) (*meshconfig.MeshConfig, error) {
	yaml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read mesh config file")
	}
	return model.ApplyMeshConfigDefaults(string(yaml))
}

// ReadMeshNetworksConfig gets mesh networks configuration from a config file
func ReadMeshNetworksConfig(filename string) (*meshconfig.MeshNetworks, error) {
	yaml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read networks config file")
	}
	return model.LoadMeshNetworksConfig(string(yaml))
}
