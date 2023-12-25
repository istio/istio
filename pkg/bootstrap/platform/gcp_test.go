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
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
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
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
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
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
		{
			"project id error",
			func() bool { return true },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPLocation: "location", GCPProjectNumber: "npid", GCPCluster: "cluster", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
			},
		},
		{
			"numeric project id error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPLocation: "location", GCPProject: "pid", GCPCluster: "cluster", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
		{
			"location error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", errors.New("error") },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPCluster: "cluster", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
			},
		},
		{
			"cluster name error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", errors.New("error") },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCEInstance: "instanceName", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
			},
		},
		{
			"instance name error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", errors.New("error") },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstanceID: "instance",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
		{
			"instance id error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
		{
			"instance template error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
		{
			"instance created by error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "", errors.New("error") },
			map[string]string{},
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
		{
			"use env variable",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{
				GCPProject:       "env_pid",
				GCPProjectNumber: "env_pn",
				GCPCluster:       "env_cluster",
				GCPLocation:      "env_location",
			},
			map[string]string{
				GCPProject: "env_pid", GCPProjectNumber: "env_pn", GCPLocation: "env_location", GCPCluster: "env_cluster",
			},
		},
		{
			"use env variable without fill",
			func() bool { return false },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			func() (string, error) { return "", nil },
			map[string]string{
				GCPProject:       "env_pid",
				GCPProjectNumber: "env_pn",
				GCPCluster:       "env_cluster",
				GCPLocation:      "env_location",
			},
			map[string]string{
				GCPProject: "env_pid", GCPProjectNumber: "env_pn", GCPLocation: "env_location", GCPCluster: "env_cluster",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &GCPStaticMetadata, tt.env)
			shouldFillMetadata, projectIDFn, numericProjectIDFn, clusterLocationFn, clusterNameFn,
				instanceNameFn, instanceIDFn, instanceTemplateFn, createdByFn = tt.shouldFill, tt.projectIDFn,
				tt.numericProjectIDFn, tt.locationFn, tt.clusterNameFn, tt.instanceNameFn, tt.instanceIDFn, tt.instanceTemplateFn, tt.instanceCreatedByFn
			e := NewGCP()
			got := e.Metadata()
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestMetadataCache(t *testing.T) {
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
		want                map[string]string
	}{
		{
			"should cache",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		}, {
			"should ignore",
			func() bool { return true },
			func() (string, error) { return "newPid", nil },
			func() (string, error) { return "newNpid", nil },
			func() (string, error) { return "newLocation", nil },
			func() (string, error) { return "newCluster", nil },
			func() (string, error) { return "newInstanceName", nil },
			func() (string, error) { return "newInstance", nil },
			func() (string, error) { return "newInstanceTemplate", nil },
			func() (string, error) { return "newCreatedBy", nil },
			map[string]string{
				GCPProject: "pid", GCPProjectNumber: "npid", GCPLocation: "location", GCPCluster: "cluster", GCEInstance: "instanceName",
				GCEInstanceID: "instance", GCEInstanceTemplate: "instanceTemplate", GCEInstanceCreatedBy: "createdBy",
				GCPClusterURL: "https://container.googleapis.com/v1/projects/pid/locations/location/clusters/cluster",
			},
		},
	}

	var gcpEnvVar Environment
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldFillMetadata, projectIDFn, numericProjectIDFn, clusterLocationFn, clusterNameFn,
				instanceNameFn, instanceIDFn, instanceTemplateFn, createdByFn = tt.shouldFill, tt.projectIDFn,
				tt.numericProjectIDFn, tt.locationFn, tt.clusterNameFn, tt.instanceNameFn, tt.instanceIDFn, tt.instanceTemplateFn, tt.instanceCreatedByFn

			if gcpEnvVar == nil {
				gcpEnvVar = NewGCP()
			}
			got := gcpEnvVar.Metadata()
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestDefaultPort(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{
			host: "foo",
			want: "foo:80",
		},
		{
			host: "foo:80",
			want: "foo:80",
		},
		{
			host: "foo:8080",
			want: "foo:8080",
		},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := defaultPort(tt.host, "80"); got != tt.want {
				t.Errorf("defaultPort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocality(t *testing.T) {
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
			"fill by env variable",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{
				GCPProject:       "env_pid",
				GCPProjectNumber: "env_pn",
				GCPCluster:       "env_cluster",
				GCPLocation:      "us-west1-ir",
			},
			map[string]string{"Zone": "us-west1-ir", "Region": "us-west1"},
		},
		{
			"no env variable",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "us-west1-ir", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{"Zone": "us-west1-ir", "Region": "us-west1"},
		},
		{
			"empty result",
			func() bool { return false },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "npid", nil },
			func() (string, error) { return "us-west1-ir", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{},
		},
		{
			"unable to reach compute metadata",
			func() bool { return true },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "instanceName", nil },
			func() (string, error) { return "instance", nil },
			func() (string, error) { return "instanceTemplate", nil },
			func() (string, error) { return "createdBy", nil },
			map[string]string{},
			map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &GCPStaticMetadata, tt.env)
			shouldFillMetadata, projectIDFn, numericProjectIDFn, clusterLocationFn, clusterNameFn,
				instanceNameFn, instanceIDFn, instanceTemplateFn, createdByFn = tt.shouldFill, tt.projectIDFn,
				tt.numericProjectIDFn, tt.locationFn, tt.clusterNameFn, tt.instanceNameFn, tt.instanceIDFn, tt.instanceTemplateFn, tt.instanceCreatedByFn

			e := NewGCP()
			got := e.Locality()
			assert.Equal(t, got.Zone, tt.want["Zone"])
			assert.Equal(t, got.Region, tt.want["Region"])
		})
	}
}
