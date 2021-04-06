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
	"testing"
)

// Mock responses for Azure Metadata (based on Microsoft API documentation samples)
// https://docs.microsoft.com/en-us/azure/virtual-machines/windows/instance-metadata-service
const (
	MockVersionsTemplate = `{"error": "Bad request. api-version was not specified in the request","newest-versions": ["%s"]}`
	MockMetadata         = `{"compute": {"location": "centralus", "name": "negasonic", "tags": "Department:IT;Environment:Prod;Role:WorkerRole", ` +
		`"vmId": "13f56399-bd52-4150-9748-7190aae1ff21", "zone": "1"}}`
	MockMetadataWithValuelessTag = `{"compute": {"location": "centralus", "name": "negasonic", "tags": "Department", ` +
		`"vmId": "13f56399-bd52-4150-9748-7190aae1ff21", "zone": "1"}}`
	MockMetadataTagsList = `{"compute": {"location": "centralus", "name": "negasonic", ` +
		`"tagsList": [{"name": "Department", "value":"IT"}, {"name": "Environment", "value": "Prod"}, {"name": "Role", "value": "WorkerRole; OtherWorker"}], ` +
		`"vmId": "13f56399-bd52-4150-9748-7190aae1ff21", "zone": "1"}}`
)

func TestAzureVersionUpdate(t *testing.T) {
	oldAzureAPIVersionsFn := azureAPIVersionsFn
	defer func() { azureAPIVersionsFn = oldAzureAPIVersionsFn }()
	MinDate, MaxDate := "0000-00-00", "9999-12-31"
	tests := []struct {
		name     string
		response string
		version  string
	}{
		{"ignore empty response", "", AzureDefaultAPIVersion},
		{"ignore smaller version", fmt.Sprintf(MockVersionsTemplate, MinDate), AzureDefaultAPIVersion},
		{"ignore no version", fmt.Sprintf(MockVersionsTemplate, ""), AzureDefaultAPIVersion},
		{"use larger version", fmt.Sprintf(MockVersionsTemplate, MaxDate), MaxDate},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			azureAPIVersionsFn = func() string { return tt.response }
			e := &azureEnv{APIVersion: AzureDefaultAPIVersion}
			e.updateAPIVersion()
			if e.APIVersion != tt.version {
				t.Errorf("updateAPIVersion() => '%v'; want '%v'", e.APIVersion, tt.version)
			}
		})
	}
}

func TestAzureMetadata(t *testing.T) {
	oldGetAPIVersions, oldGetAzureMetadata := azureAPIVersionsFn, azureMetadataFn
	defer func() { azureAPIVersionsFn, azureMetadataFn = oldGetAPIVersions, oldGetAzureMetadata }()
	tests := []struct {
		name     string
		response string
		metadata map[string]string
	}{
		{"ignore empty response", "", map[string]string{}},
		{
			"parse fields", MockMetadata,
			map[string]string{
				"azure_Department": "IT", "azure_Environment": "Prod", "azure_Role": "WorkerRole",
				"azure_name": "negasonic", "azure_location": "centralus", "azure_vmId": "13f56399-bd52-4150-9748-7190aae1ff21",
			},
		},
		{
			"handle tags without values", MockMetadataWithValuelessTag,
			map[string]string{
				"azure_Department": "", "azure_name": "negasonic", "azure_location": "centralus", "azure_vmId": "13f56399-bd52-4150-9748-7190aae1ff21",
			},
		},
		{
			"handle tagsList", MockMetadataTagsList,
			map[string]string{
				"azure_Department": "IT", "azure_Environment": "Prod", "azure_Role": "WorkerRole; OtherWorker",
				"azure_name": "negasonic", "azure_location": "centralus", "azure_vmId": "13f56399-bd52-4150-9748-7190aae1ff21",
			},
		},
	}

	// Prevent actual requests to metadata server for updating the API version
	azureAPIVersionsFn = func() string { return "" }
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			azureMetadataFn = func(string) string { return tt.response }
			e := NewAzure()
			if metadata := e.Metadata(); !reflect.DeepEqual(metadata, tt.metadata) {
				t.Errorf("Metadata() => '%v'; want '%v'", metadata, tt.metadata)
			}
		})
	}
}
