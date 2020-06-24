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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/pkg/log"
)

const (
	AzureMetadataEndpoint = "http://169.254.169.254"
	InstanceUrl           = AzureMetadataEndpoint + "/metadata/instance"
	DefaultAPIVersion     = "2019-08-15"
	SysVendorPath         = "/sys/class/dmi/id/sys_vendor"
	MicrosoftIdentifier   = "Microsoft Corporation"
)

var (
	APIVersion        = DefaultAPIVersion
	updateVersionOnce = sync.Once{}
	getAPIVersions    = func() string {
		return metadataRequest("")
	}
	getAzureMetadata = func() string {
		return metadataRequest(fmt.Sprintf("api-version=%s", APIVersion))
	}

	azureTagsFn = func(e *azureEnv) map[string]string {
		tags := map[string]string{}
		if at, ok := e.computeMetadata["tags"]; ok && len(at.(string)) > 0 {
			for _, tag := range strings.Split(at.(string), ";") {
				kv := strings.Split(tag, ":")
				tags[kv[0]] = kv[1]
			}
		}
		return tags
	}
	azureLocationFn = func(e *azureEnv) string {
		if al, ok := e.computeMetadata["location"]; ok {
			return al.(string)
		}
		return ""
	}
	azureZoneFn = func(e *azureEnv) string {
		if az, ok := e.computeMetadata["zone"]; ok {
			return az.(string)
		}
		return ""
	}
)

type azureEnv struct {
	computeMetadata map[string]interface{}
	networkMetadata map[string]interface{}
}

// IsAzure returns whether or not the platform for bootstrapping is Azure
func IsAzure() bool {
	sysVendor, err := ioutil.ReadFile(SysVendorPath)
	if err != nil {
		log.Warnf("Error reading sys_vendor in Azure platform detection: %v", err)
	}
	return strings.Contains(string(sysVendor), MicrosoftIdentifier)
}

// Attempts to update the API version
func updateAPIVersion() {
	updateVersionOnce.Do(func() {
		bodyJson := stringToJson(getAPIVersions())
		if newestVersions, ok := bodyJson["newest-versions"]; ok {
			for _, version := range newestVersions.([]interface{}) {
				if strings.Compare(version.(string), APIVersion) > 0 {
					APIVersion = version.(string)
				}
			}
		}
	})
}

// NewAzure returns a platform environment for Azure
func NewAzure() Environment {
	updateAPIVersion()
	e := &azureEnv{}
	e.getMetadata()
	return e
}

// Retrieves Azure instance metadata response body stores it in the Azure environment
func (e *azureEnv) getMetadata() {
	bodyJson := stringToJson(getAzureMetadata())
	if computeMetadata, ok := bodyJson["compute"]; ok {
		e.computeMetadata = computeMetadata.(map[string]interface{})
	}
	if networkMetadata, ok := bodyJson["network"]; ok {
		e.networkMetadata = networkMetadata.(map[string]interface{})
	}
}

// Generic Azure metadata GET request helper for the response body
// Uses the default timeout for the HTTP get request
func metadataRequest(query string) string {
	client := http.Client{Timeout: defaultTimeout}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s?%s", InstanceUrl, query), nil)
	if err != nil {
		log.Warnf("Failed to create HTTP request: %v", err)
		return ""
	}
	req.Header.Add("Metadata", "True")

	response, err := client.Do(req)
	if err != nil {
		log.Warnf("HTTP request failed: %v", err)
		return ""
	}
	if response.StatusCode != http.StatusOK {
		log.Warnf("HTTP request unsuccessful with status: %v", response.Status)
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Warnf("Could not read response body: %v", err)
		return ""
	}
	return string(body)
}

func stringToJson(s string) map[string]interface{} {
	var sJson map[string]interface{}
	if err := json.Unmarshal([]byte(s), &sJson); err != nil {
		log.Warnf("Could not unmarshal response: %v:", err)
	}
	return sJson
}

// Returns Azure instance metadata. Must be run on an Azure VM
func (e *azureEnv) Metadata() map[string]string {
	md := map[string]string{}
	for k, v := range azureTagsFn(e) {
		md[k] = v
	}
	return md
}

// Locality returns the region and zone
func (e *azureEnv) Locality() *core.Locality {
	var l core.Locality
	l.Region = azureLocationFn(e)
	l.Zone = azureZoneFn(e)
	return &l
}
