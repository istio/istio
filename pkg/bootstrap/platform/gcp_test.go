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
		instanceNameFn      metadataFn
		instanceIDFn        metadataFn
		instanceTemplateFn  metadataFn
		instanceCreatedByFn metadataFn
		env                 map[string]string
		want                map[string]string
	}{
		{
			"should not fill",
			func() bool { return false },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{},
		},
		{
			"should fill",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"project id error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPLocation: "location", GCPProjectNumber: "npid", GCPCluster: "cluster", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"numeric project id error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPLocation: "location", GCPProject: "pid", GCPCluster: "cluster", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"location error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPCluster: "cluster", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"cluster name error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance name error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance id error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance template error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "", errors.New("error") },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceCreatedBy: "createdBy"},
		},
		{
			"instance created by error",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "", errors.New("error") },
			map[string]string{},
			map[string]string{GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate"},
		},
		{
			"use env variable",
			func() bool { return true },
			func(e *gcpEnv) (string, error) { return "pid", nil },
			func(e *gcpEnv) (string, error) { return "npid", nil },
			func(e *gcpEnv) (string, error) { return "location", nil },
			func(e *gcpEnv) (string, error) { return "cluster", nil },
			func(e *gcpEnv) (string, error) { return "instanceName", nil },
			func(e *gcpEnv) (string, error) { return "instance", nil },
			func(e *gcpEnv) (string, error) { return "instanceTemplate", nil },
			func(e *gcpEnv) (string, error) { return "createdBy", nil },
			map[string]string{"GCP_METADATA": "env_pid|env_pn|env_cluster|env_location"},
			map[string]string{GCPProject: "env_pid", GCPProjectNumber: "env_pn", GCPLocation: "env_location", GCPCluster: "env_cluster",
				GCEInstance: "instanceName", GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			for e, v := range tt.env {
				os.Setenv(e, v)
			}
			envOnce = sync.Once{}
			e := gcpEnv{}
			shouldFillMetadata, projectIDFn, numericProjectIDFn, clusterLocationFn, clusterNameFn, instanceNameFn, instanceIDFn, instanceTemplateFn, createdByFn =
				tt.shouldFill, tt.projectIDFn, tt.numericProjectIDFn, tt.locationFn, tt.clusterNameFn, tt.instanceNameFn, tt.instanceIDFn, tt.instanceTemplateFn,
				tt.instanceCreatedByFn
			got := e.Metadata()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gcpEnv.Metadata() => '%v'; want '%v'", got, tt.want)
			}
		})
	}
}

func TestCacheMetadata(t *testing.T) {
	tests := []struct {
		name string
		fVal string
		fErr error
		want string
	}{
		{"ignore bad response", "err", errors.New("err"), ""},
		{"properly update field", "test1", nil, "test1"},
		{"return cached val", "ignore", nil, "test1"},
	}

	// Use same gcpEnv for sequential tests
	e := gcpEnv{}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			f := func() (string, error) { return tt.fVal, tt.fErr }
			cb, _ := getCacheMetadata(&e.createdBy, f)
			if cb != tt.want || e.createdBy != tt.want {
				t.Errorf("getCacheMetadata() => returned: '%v', attribute: '%v'; want '%v'", cb, e.createdBy, tt.want)
			}
		})
	}
}
