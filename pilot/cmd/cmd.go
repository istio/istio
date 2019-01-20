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
	"fmt"
	"io/ioutil"
	"strings"

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

// Validate checks the field values on Cluster with the rules defined in the
// proto definition for this message. If any rules are violated, an error is returned.
func ValidateMeshConfig(mesh *meshconfig.MeshConfig) error {
	if mesh == nil || mesh.LocalityLbSetting == nil {
		return nil
	}

	lb := mesh.LocalityLbSetting

	if len(lb.GetDistribute()) > 0 && len(lb.GetFailover()) > 0 {
		return fmt.Errorf("can not simultaneously specify 'distribute' and 'failover'")
	}

	srcLocalities := []string{}
	for _, locality := range lb.GetDistribute() {
		srcLocalities = append(srcLocalities, locality.From)
		var totalWeight uint32
		destLocalities := []string{}
		for loc, weight := range locality.To {
			destLocalities = append(destLocalities, loc)
			if weight == 0 {
				return fmt.Errorf("locality weight must not be in range [1, 100]")
			}
			totalWeight += weight
		}
		if totalWeight != 100 {
			return fmt.Errorf("total locality weight %v != 100", totalWeight)
		}
		if err := validateLocalities(destLocalities); err != nil {
			return err
		}
	}

	if err := validateLocalities(srcLocalities); err != nil {
		return err
	}

	for _, failover := range lb.GetFailover() {
		if failover.From == failover.To {
			return fmt.Errorf("locality lb failover settings must specify different regions")
		}
		if strings.Contains(failover.To, "*") {
			return fmt.Errorf("locality lb failover region should not contain '*' wildcard")
		}
	}

	return nil
}

func validateLocalities(localities []string) error {
	regionZoneSubZoneMap := map[string]map[string]map[string]bool{}

	for _, locality := range localities {
		if n := strings.Count(locality, "*"); n > 0 {
			if n > 1 || !strings.HasSuffix(locality, "*") {
				return fmt.Errorf("locality %s wildcard '*' number can not exceed 1 and must be in the end", locality)
			}
		}

		items := strings.SplitN(locality, "/", 3)
		for _, item := range items {
			if item == "" {
				return fmt.Errorf("locality %s must not contain empty region/zone/subzone info", locality)
			}
		}
		if _, ok := regionZoneSubZoneMap["*"]; ok {
			return fmt.Errorf("locality %s overlap with previous specified ones", locality)
		}
		switch len(items) {
		case 1:
			if _, ok := regionZoneSubZoneMap[items[0]]; ok {
				return fmt.Errorf("locality %s overlap with previous specified ones", locality)
			}
			regionZoneSubZoneMap[items[0]] = map[string]map[string]bool{"*": {"*": true}}
		case 2:
			if _, ok := regionZoneSubZoneMap[items[0]]; ok {
				if _, ok := regionZoneSubZoneMap[items[0]]["*"]; ok {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				if _, ok := regionZoneSubZoneMap[items[0]][items[1]]; ok {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				regionZoneSubZoneMap[items[0]][items[1]] = map[string]bool{"*": true}
			} else {
				regionZoneSubZoneMap[items[0]] = map[string]map[string]bool{items[1]: {"*": true}}
			}
		case 3:
			if _, ok := regionZoneSubZoneMap[items[0]]; ok {
				if _, ok := regionZoneSubZoneMap[items[0]]["*"]; ok {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				if _, ok := regionZoneSubZoneMap[items[0]][items[1]]; ok {
					if regionZoneSubZoneMap[items[0]][items[1]]["*"] {
						return fmt.Errorf("locality %s overlap with previous specified ones", locality)
					}
					if regionZoneSubZoneMap[items[0]][items[1]][items[2]] {
						return fmt.Errorf("locality %s overlap with previous specified ones", locality)
					}
					regionZoneSubZoneMap[items[0]][items[1]][items[2]] = true
				} else {
					regionZoneSubZoneMap[items[0]][items[1]] = map[string]bool{items[2]: true}
				}
			} else {
				regionZoneSubZoneMap[items[0]] = map[string]map[string]bool{items[1]: {items[2]: true}}
			}
		}
	}

	return nil
}
