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
	"testing"
)

func mockVersion() []string {
	return []string{"9999-12-31"}
}

// Returns a sample response json (from Microsoft API documentation)
func mockMetadata() string {
	mockJson := "{\n  \"compute\": {\n    \"azEnvironment\": \"AzurePublicCloud\",\n    \"customData\": \"\",\n    \"location\": \"centralus\",\n    \"name\": \"negasonic\",\n    \"offer\": \"lampstack\",\n    \"osType\": \"Linux\",\n    \"placementGroupId\": \"\",\n    \"plan\": {\n        \"name\": \"5-6\",\n        \"product\": \"lampstack\",\n        \"publisher\": \"bitnami\"\n    },\n    \"platformFaultDomain\": \"0\",\n    \"platformUpdateDomain\": \"0\",\n    \"provider\": \"Microsoft.Compute\",\n    \"publicKeys\": [],\n    \"publisher\": \"bitnami\",\n    \"resourceGroupName\": \"myrg\",\n    \"resourceId\": \"/subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/resourceGroups/myrg/providers/Microsoft.Compute/virtualMachines/negasonic\",\n    \"sku\": \"5-6\",\n    \"storageProfile\": {\n        \"dataDisks\": [\n          {\n            \"caching\": \"None\",\n            \"createOption\": \"Empty\",\n            \"diskSizeGB\": \"1024\",\n            \"image\": {\n              \"uri\": \"\"\n            },\n            \"lun\": \"0\",\n            \"managedDisk\": {\n              \"id\": \"/subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/resourceGroups/macikgo-test-may-23/providers/Microsoft.Compute/disks/exampledatadiskname\",\n              \"storageAccountType\": \"Standard_LRS\"\n            },\n            \"name\": \"exampledatadiskname\",\n            \"vhd\": {\n              \"uri\": \"\"\n            },\n            \"writeAcceleratorEnabled\": \"false\"\n          }\n        ],\n        \"imageReference\": {\n          \"id\": \"\",\n          \"offer\": \"UbuntuServer\",\n          \"publisher\": \"Canonical\",\n          \"sku\": \"16.04.0-LTS\",\n          \"version\": \"latest\"\n        },\n        \"osDisk\": {\n          \"caching\": \"ReadWrite\",\n          \"createOption\": \"FromImage\",\n          \"diskSizeGB\": \"30\",\n          \"diffDiskSettings\": {\n            \"option\": \"Local\"\n          },\n          \"encryptionSettings\": {\n            \"enabled\": \"false\"\n          },\n          \"image\": {\n            \"uri\": \"\"\n          },\n          \"managedDisk\": {\n            \"id\": \"/subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/resourceGroups/macikgo-test-may-23/providers/Microsoft.Compute/disks/exampleosdiskname\",\n            \"storageAccountType\": \"Standard_LRS\"\n          },\n          \"name\": \"exampleosdiskname\",\n          \"osType\": \"Linux\",\n          \"vhd\": {\n            \"uri\": \"\"\n          },\n          \"writeAcceleratorEnabled\": \"false\"\n        }\n    },\n    \"subscriptionId\": \"xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx\",\n    \"tags\": \"Department:IT;Environment:Prod;Role:WorkerRole\",\n    \"version\": \"7.1.1902271506\",\n    \"vmId\": \"13f56399-bd52-4150-9748-7190aae1ff21\",\n    \"vmScaleSetName\": \"\",\n    \"vmSize\": \"Standard_A1_v2\",\n    \"zone\": \"1\"\n  },\n  \"network\": {\n    \"interface\": [\n      {\n        \"ipv4\": {\n          \"ipAddress\": [\n            {\n              \"privateIpAddress\": \"10.1.2.5\",\n              \"publicIpAddress\": \"X.X.X.X\"\n            }\n          ],\n          \"subnet\": [\n            {\n              \"address\": \"10.1.2.0\",\n              \"prefix\": \"24\"\n            }\n          ]\n        },\n        \"ipv6\": {\n          \"ipAddress\": []\n        },\n        \"macAddress\": \"000D3A36DDED\"\n      }\n    ]\n  }\n}"
	return mockJson
}

func TestAzureMetadata(t *testing.T) {
	getAPIVersions = mockVersion
	getAzureMetadata = mockMetadata
	e := NewAzure()
	e.Metadata()
	e.Locality()
}
