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

package util

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	kube2 "istio.io/istio/pkg/config/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
)

var fieldMap = map[string]int{
	"{.metadata.name}":                                              1,
	"{.metadata.namespace}":                                         1,
	"{.metadata.annotations.test}":                                  1,
	"{.spec.test[0].route[0].destination.host}":                     1,
	"{.spec.http[0].mirror.host}":                                   1,
	"{.spec.gateways[0]}":                                           1,
	"{.spec.http[0].match[0].test.regex}":                           1,
	"{.spec.http[0].match[0].test.test.regex}":                      1,
	"{.spec.http[0].corsPolicy.allowOrigins[0].regex}":              1,
	"{.spec.workloadSelector.labels.test}":                          1,
	"{.spec.ports[0].port}":                                         1,
	"{.spec.containers[0].image}":                                   1,
	"{.spec.rules[0].from[0].source.namespaces[0]}":                 1,
	"{.spec.selector.test}":                                         1,
	"{.spec.servers[0].tls.credentialName}":                         1,
	"{.networks.test.endpoints[0]}":                                 1,
	"{.spec.trafficPolicy.tls.caCertificates}":                      1,
	"{.spec.trafficPolicy.portLevelSettings[0].tls.caCertificates}": 1,
	"{.spec.configPatches[0].patch.value}":                          1,
}

func TestExtractLabelFromSelectorString(t *testing.T) {
	g := NewWithT(t)
	s := "label=test"
	g.Expect(ExtractLabelFromSelectorString(s)).To(Equal("label"))
}

func TestErrorLine(t *testing.T) {
	g := NewWithT(t)
	r := &resource.Instance{Origin: &kube2.Origin{FieldsMap: fieldMap}}
	test1, err1 := ErrorLine(r, "{.metadata.name}")
	test2, err2 := ErrorLine(r, "{.metadata.fake}")
	g.Expect(test1).To(Equal(1))
	g.Expect(err1).To(Equal(true))
	g.Expect(test2).To(Equal(0))
	g.Expect(err2).To(Equal(false))
}

func TestConstants(t *testing.T) {
	g := NewWithT(t)

	constantsPath := []string{
		fmt.Sprintf(DestinationHost, "test", 0, 0),
		fmt.Sprintf(MirrorHost, 0),
		fmt.Sprintf(VSGateway, 0),
		fmt.Sprintf(URISchemeMethodAuthorityRegexMatch, 0, 0, "test"),
		fmt.Sprintf(HeaderAndQueryParamsRegexMatch, 0, 0, "test", "test"),
		fmt.Sprintf(AllowOriginsRegexMatch, 0, 0),
		fmt.Sprintf(WorkloadSelector, "test"),
		fmt.Sprintf(PortInPorts, 0),
		fmt.Sprintf(FromRegistry, "test", 0),
		fmt.Sprintf(ImageInContainer, 0),
		fmt.Sprintf(AuthorizationPolicyNameSpace, 0, 0, 0),
		fmt.Sprintf(Annotation, "test"),
		fmt.Sprintf(GatewaySelector, "test"),
		fmt.Sprintf(CredentialName, 0),
		fmt.Sprintf(DestinationRuleTLSPortLevelCert, 0),
		MetadataNamespace,
		MetadataName,
		DestinationRuleTLSCert,
		fmt.Sprintf(EnvoyFilterConfigPath, 0),
	}

	for _, v := range constantsPath {
		g.Expect(fieldMap[v]).To(Equal(1))
	}
}
