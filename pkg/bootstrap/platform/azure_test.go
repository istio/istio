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
	"fmt"
	"reflect"
	"sync"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// Mock responses for Azure Metadata (based on Microsoft API documentation samples)
// https://docs.microsoft.com/en-us/azure/virtual-machines/windows/instance-metadata-service
const (
	MockVersionsTemplate = "{\"error\": \"Bad request. api-version was not specified in the request\",\"newest-versions\": [\"%s\"]}"
	MockMetadata         = "{\"compute\": {\"location\": \"centralus\", \"name\": \"negasonic\", \"tags\": \"Department:IT;Environment:Prod;Role:WorkerRole\", \"vmId\": \"13f56399-bd52-4150-9748-7190aae1ff21\", \"zone\": \"1\"}}"
)

func TestVersionUpdate(t *testing.T) {
	oldGetAPIVersions, oldAPIVersion := getAPIVersions, APIVersion
	defer func() { getAPIVersions, APIVersion = oldGetAPIVersions, oldAPIVersion }()
	MinDate, MaxDate := "0000-00-00", "9999-12-31"
	tests := []struct {
		name     string
		response string
		version  string
	}{
		{"ignore empty response", "", oldAPIVersion},
		{"ignore smaller version", fmt.Sprintf(MockVersionsTemplate, MinDate), oldAPIVersion},
		{"ignore no version", fmt.Sprintf(MockVersionsTemplate, ""), oldAPIVersion},
		{"use larger version", fmt.Sprintf(MockVersionsTemplate, MaxDate), MaxDate},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			getAPIVersions = func() string { return tt.response }
			updateVersionOnce = sync.Once{}
			updateAPIVersion()
			if APIVersion != tt.version {
				t.Errorf("updateAPIVersion() => '%v'; want '%v'", APIVersion, tt.version)
			}
		})
	}
}

func TestAzureMetadata(t *testing.T) {
	oldGetAPIVersions, oldGetAzureMetadata := getAPIVersions, getAzureMetadata
	defer func() { getAPIVersions, getAzureMetadata = oldGetAPIVersions, oldGetAzureMetadata }()
	tests := []struct {
		name     string
		response string
		metadata map[string]string
		locality core.Locality
	}{
		{"ignore empty response", "", map[string]string{}, core.Locality{}},
		{"parse fields", MockMetadata,
			map[string]string{"Department": "IT", "Environment": "Prod", "Role": "WorkerRole"},
			core.Locality{Region: "centralus", Zone: "1"}},
	}

	// Prevent actual requests to metadata server for updating the API version
	getAPIVersions = func() string { return "" }
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			getAzureMetadata = func() string { return tt.response }
			e := NewAzure()
			if metadata := e.Metadata(); !reflect.DeepEqual(metadata, tt.metadata) {
				t.Errorf("Metadata() => '%v'; want '%v'", metadata, tt.metadata)
			}
			if locality := *e.Locality(); !reflect.DeepEqual(locality, tt.locality) {
				t.Errorf("Locality() => '%v'; want '%v'", locality, tt.locality)
			}
		})
	}
}
