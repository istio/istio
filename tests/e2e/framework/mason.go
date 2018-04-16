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

package framework

import (
	"fmt"
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

type instanceInfo struct {
	Name string `json:"name"`
	Zone string `json:"zone"`
}

// resourceInfo holds information about the resource created, such that it can used
type resourceInfo struct {
	ProjectsInfo []struct {
		Name     string         `json:"name"`
		Clusters []instanceInfo `json:"clusters,omitempty"`
		VMs      []instanceInfo `json:"vms,omitempty"`
	} `json:"projectsinfo,omitempty"`
}

func parseInfoFile(filePath string) (*resourceInfo, error) {
	var info resourceInfo

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if err = yaml.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func resourceInfoToGCPRawVM(info resourceInfo, ns string) (*GCPRawVM, error) {
	// Need to have the cluster and VM are in the same project,
	// if there is more than one project we'll take the first one.
	if len(info.ProjectsInfo) < 1 {
		return nil, fmt.Errorf("there should be at least one project")
	}
	projectInfo := info.ProjectsInfo[0]
	// If more than one VM, taking the first one
	if len(projectInfo.VMs) < 1 {
		return nil, fmt.Errorf("there should be at least one vm")
	}
	vmInfo := projectInfo.VMs[0]
	// If more than one Cluster, taking the first one
	if len(projectInfo.Clusters) < 1 {
		return nil, fmt.Errorf("there should be at least one cluster")
	}
	clusterInfo := projectInfo.Clusters[0]
	return &GCPRawVM{
		Name:        vmInfo.Name,
		Zone:        vmInfo.Zone,
		ClusterName: clusterInfo.Name,
		ProjectID:   projectInfo.Name,
		UseMason:    true,
		Namespace:   ns,
	}, nil
}
