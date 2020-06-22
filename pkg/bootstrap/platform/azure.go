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
	"io/ioutil"
	"net/http"
	"strings"

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
	// Attempts to update the API version
	updateAPIVersion = func(e *azureEnv) {
		for _, version := range getAPIVersions() {
			if strings.Compare(version, e.APIVersion) > 0 {
				e.APIVersion = version
			}
		}
	}
	getAPIVersions = func() []string {
		versions := []string{}
		client := http.Client{Timeout: defaultTimeout}
		req, err := http.NewRequest("GET", InstanceUrl, nil)
		if err != nil {
			log.Warnf("Failed to create HTTP request: %v", err)
			return versions
		}
		req.Header.Add("Metadata", "True")

		response, err := client.Do(req)
		if err != nil {
			log.Warnf("HTTP request failed: %v", err)
			return versions
		}
		if response.StatusCode != http.StatusOK {
			log.Warnf("HTTP request unsuccessful with status: %v", response.Status)
		}
		defer response.Body.Close()
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Warnf("Could not read response body: %v", err)
			return versions
		}
		bodyJson := &map[string]interface{}{}
		if err = json.Unmarshal(body, bodyJson); err != nil {
			log.Warnf("Could not unmarshal response: %v:", err)
			return versions
		}
		if newestVersions, ok := (*bodyJson)["newest-versions"]; ok {
			versions = newestVersions.([]string)
		}
		return versions
	}

	// Retrieves Azure instance metadata and stores it in the Azure environment
	// Uses the default timeout for the HTTP get request
	azureMetadataFn = func(e *azureEnv) {
		metadata := &map[string]interface{}{}
		body := []byte(getAzureMetadata())
		if err := json.Unmarshal(body, metadata); err != nil {
			log.Warnf("Could not unmarshal response: %v:", err)
			return
		}
		if computeMetadata, ok := (*metadata)["compute"]; ok {
			e.computeMetadata = computeMetadata.(map[string]interface{})
		}
		if networkMetadata, ok := (*metadata)["network"]; ok {
			e.networkMetadata = networkMetadata.(map[string]interface{})
		}
	}
	getAzureMetadata = func() string {
		client := &http.Client{Timeout: defaultTimeout}
		req, err := http.NewRequest("GET", InstanceUrl, nil)
		if err != nil {
			log.Warnf("Failed to create HTTP request: %v", err)
			return ""
		}
		req.Header.Add("Metadata", "True")
		query := req.URL.Query()
		query.Add("api-version", DefaultAPIVersion)
		query.Add("format", "json")
		req.URL.RawQuery = query.Encode()

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
	APIVersion      string
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

// NewAzure returns a platform environment for Azure
func NewAzure() Environment {
	e := &azureEnv{APIVersion: DefaultAPIVersion}
	updateAPIVersion(e)
	azureMetadataFn(e)
	return e
}

// Metadata returns Azure instance metadata
// Must be run from within an Azure VM
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
