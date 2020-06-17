// Copyright Istio Authors
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

package platform

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
)

func TestGCPMetadata(t *testing.T) {
	tests := []struct {
		name                string
		shouldFill          shouldFillFn
		projectIDFn         metadataFn
		numericProjectIDFn  metadataFn
		locationFn          metadataFn
		clusterNameFn       metadataFn
		instanceIDFn        metadataFn
		instanceTemplateFn  metadataFn
		instanceCreatedByFn metadataFn
		env                 map[string]string
		want                map[string]string
	}{
		{
			"should not fill",
			func() bool { return false },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{},
		},
		{
			"should fill",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"project id error",
			func() bool { return true },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPLocation: "location", GCPProjectNumber: "npid", GCPCluster: "cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"numeric project id error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPLocation: "location", GCPProject: "pid", GCPCluster: "cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"location error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", errors.New("error") },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPCluster: "cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"cluster name error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", errors.New("error") },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance id error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance template error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster",
				GCEInstanceID: "instance", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance created by error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "", errors.New("error") },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate"},
		},
		{
			"use env variable",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{"GCP_METADATA": "env_pid|env_pn|env_cluster|env_location"},
			map[string]string{GCPProject: "env_pid", GCPProjectNumber: "env_pn", GCPLocation: "env_location", GCPCluster: "env_cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			for e, v := range tt.env {
				os.Setenv(e, v)
			}
			once = sync.Once{}
			mg := gcpEnv{tt.shouldFill, tt.projectIDFn, tt.numericProjectIDFn, tt.locationFn, tt.clusterNameFn, tt.instanceIDFn,
				tt.instanceTemplateFn, tt.instanceCreatedByFn}
			got := mg.Metadata()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gcpEnv.Metadata() => '%v'; want '%v'", got, tt.want)
			}
		})
	}
}
