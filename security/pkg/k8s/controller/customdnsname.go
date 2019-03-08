// Copyright 2019 Istio Authors
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

package controller

import (
	"strings"

	"istio.io/istio/pkg/log"
)

func ConstructCustomDNSNames(serviceAccounts []string, serviceNames []string,
	namespace string, customDNSNames string) map[string]*DNSNameEntry {
	result := make(map[string]*DNSNameEntry)
	for i, svcAccount := range serviceAccounts {
		result[svcAccount] = &DNSNameEntry{
			ServiceName: serviceNames[i],
			Namespace:   namespace,
		}
	}
	if len(customDNSNames) > 0 {
		customNames := strings.Split(customDNSNames, ",")
		log.Infof("The custom-defined DNS name list is %v", customNames)
		for _, customName := range customNames {
			nameDomain := strings.Split(customName, ":")
			if len(nameDomain) == 2 {
				override, ok := result[nameDomain[0]]
				if ok {
					// There is already an entry for nameDomain[0], we just need to
					// append current value.
					override.CustomDomains = append(override.CustomDomains, nameDomain[1])
				} else {
					// There is no entry for nameDomain[0] in the map, create a new one.
					result[nameDomain[0]] = &DNSNameEntry{
						ServiceName:   nameDomain[0],
						CustomDomains: []string{nameDomain[1]},
					}
				}
			} else {
				log.Warnf("Cannot process this invalid custom defined names %v, it "+
					"should follow SERVICE_ACCOUNT.NAMESPACE:DOMAIN format", customName)
			}
		}
	}
	return result
}
