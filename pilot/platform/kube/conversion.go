// Copyright 2017 Istio Authors
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

package kube

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/protobuf/proto"

	multierror "github.com/hashicorp/go-multierror"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
)

// camelCaseToKabobCase converts "MyName" to "my-name"
func camelCaseToKabobCase(s string) string {
	var out bytes.Buffer
	for i := range s {
		if 'A' <= s[i] && s[i] <= 'Z' {
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteByte(s[i] - 'A' + 'a')
		} else {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

// kindToAPIName converts Kind name to 3rd party API group
func kindToAPIName(s string) string {
	return camelCaseToKabobCase(s) + "." + IstioAPIGroup
}

func convertTags(obj meta_v1.ObjectMeta) model.Tags {
	out := make(model.Tags, len(obj.Labels))
	for k, v := range obj.Labels {
		out[k] = v
	}
	return out
}

func convertPort(port v1.ServicePort) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Port),
		Protocol: convertProtocol(port.Name, port.Protocol),
	}
}

func convertService(svc v1.Service, domainSuffix string) *model.Service {
	addr, external := "", ""
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		addr = svc.Spec.ClusterIP
	}

	if svc.Spec.Type == v1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
		external = svc.Spec.ExternalName
	}

	// must have address or be external (but not both)
	if (addr == "" && external == "") || (addr != "" && external != "") {
		return nil
	}

	ports := make([]*model.Port, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		ports = append(ports, convertPort(port))
	}

	return &model.Service{
		Hostname:     serviceHostname(svc.Name, svc.Namespace, domainSuffix),
		Ports:        ports,
		Address:      addr,
		ExternalName: external,
	}
}

// serviceHostname produces FQDN for a k8s service
func serviceHostname(name, namespace, domainSuffix string) string {
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, domainSuffix)
}

// parseHostname extracts service name and namespace from the service hostnamei
func parseHostname(hostname string) (name string, namespace string, err error) {
	parts := strings.Split(hostname, ".")
	if len(parts) < 2 {
		err = fmt.Errorf("missing service name and namespace from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	namespace = parts[1]
	return
}

func convertProtocol(name string, proto v1.Protocol) model.Protocol {
	out := model.ProtocolTCP
	switch proto {
	case v1.ProtocolUDP:
		out = model.ProtocolUDP
	case v1.ProtocolTCP:
		prefix := name
		i := strings.Index(name, "-")
		if i >= 0 {
			prefix = name[:i]
		}
		switch prefix {
		case "grpc":
			out = model.ProtocolGRPC
		case "http":
			out = model.ProtocolHTTP
		case "http2":
			out = model.ProtocolHTTP2
		case "https":
			out = model.ProtocolHTTPS
		}
	}
	return out
}

// modelToKube translates Istio config to k8s config JSON
func modelToKube(km model.KindMap, k *model.Key, v proto.Message) (*Config, error) {
	kind := km[k.Kind]
	spec, err := kind.ToJSONMap(v)
	if err != nil {
		return nil, err
	}
	out := &Config{
		TypeMeta: meta_v1.TypeMeta{
			Kind: IstioKind,
		},
		Metadata: meta_v1.ObjectMeta{
			Name:      k.Kind + "-" + k.Name,
			Namespace: k.Namespace,
		},
		Spec: spec,
	}

	return out, nil
}

func convertIngress(ingress v1beta1.Ingress, domainSuffix string) map[model.Key]proto.Message {
	messages := make(map[model.Key]proto.Message)

	keyOf := func(ruleNum, pathNum int) model.Key {
		return model.Key{
			Kind:      model.IngressRule,
			Name:      encodeIngressRuleName(ingress.Name, ruleNum, pathNum),
			Namespace: ingress.Namespace,
		}
	}

	tls := ""
	if len(ingress.Spec.TLS) > 0 {
		// due to lack of listener SNI in the proxy, we only support a single secret and ignore secret hosts
		secret := ingress.Spec.TLS[0]
		tls = fmt.Sprintf("%s.%s", secret.SecretName, ingress.Namespace)
	}

	if ingress.Spec.Backend != nil {
		messages[keyOf(0, 0)] = createIngressRule("", "", ingress.Namespace, domainSuffix, *ingress.Spec.Backend, tls)
	}

	for i, rule := range ingress.Spec.Rules {
		for j, path := range rule.HTTP.Paths {
			messages[keyOf(i+1, j+1)] = createIngressRule(rule.Host, path.Path, ingress.Namespace,
				domainSuffix, path.Backend, tls)
		}
	}

	return messages
}

func createIngressRule(host, path, namespace, domainSuffix string,
	backend v1beta1.IngressBackend, tlsSecret string) proto.Message {
	destination := serviceHostname(backend.ServiceName, namespace, domainSuffix)
	tags := make(map[string]string, 2)
	switch backend.ServicePort.Type {
	case intstr.Int:
		tags[model.IngressPortNum] = strconv.Itoa(backend.ServicePort.IntValue())
	case intstr.String:
		tags[model.IngressPortName] = backend.ServicePort.String()
	}
	tags[model.IngressTLSSecret] = tlsSecret
	rule := &proxyconfig.RouteRule{
		Destination: destination,
		Match: &proxyconfig.MatchCondition{
			HttpHeaders: make(map[string]*proxyconfig.StringMatch, 2),
		},
		Route: []*proxyconfig.DestinationWeight{{Tags: tags}},
	}

	if host != "" {
		rule.Match.HttpHeaders[model.HeaderAuthority] = &proxyconfig.StringMatch{
			MatchType: &proxyconfig.StringMatch_Exact{Exact: host},
		}
	}

	if path != "" {
		if isRegularExpression(path) {
			if strings.HasSuffix(path, ".*") && !isRegularExpression(strings.TrimSuffix(path, ".*")) {
				rule.Match.HttpHeaders[model.HeaderURI] = &proxyconfig.StringMatch{
					MatchType: &proxyconfig.StringMatch_Prefix{Prefix: strings.TrimSuffix(path, ".*")},
				}
			} else {
				rule.Match.HttpHeaders[model.HeaderURI] = &proxyconfig.StringMatch{
					MatchType: &proxyconfig.StringMatch_Regex{Regex: path},
				}
			}
		} else {
			rule.Match.HttpHeaders[model.HeaderURI] = &proxyconfig.StringMatch{
				MatchType: &proxyconfig.StringMatch_Exact{Exact: path},
			}
		}
	}

	return rule
}

// encodeIngressRuleName encodes an ingress rule name for a given ingress resource name,
// as well as the position of the rule and path specified within it, counting from 1.
// ruleNum == pathNum == 0 indicates the default backend specified for an ingress.
func encodeIngressRuleName(ingressName string, ruleNum, pathNum int) string {
	return fmt.Sprintf("%s-%d-%d", ingressName, ruleNum, pathNum)
}

// decodeIngressRuleName decodes an ingress rule name previously encoded with encodeIngressRuleName.
func decodeIngressRuleName(name string) (ingressName string, ruleNum, pathNum int, err error) {
	parts := strings.Split(name, "-")
	if len(parts) < 3 {
		err = fmt.Errorf("could not decode string into ingress rule name: %s", name)
		return
	}

	pathNum, pathErr := strconv.Atoi(parts[len(parts)-1])
	ruleNum, ruleErr := strconv.Atoi(parts[len(parts)-2])

	if pathErr != nil || ruleErr != nil {
		err = multierror.Append(
			fmt.Errorf("could not decode string into ingress rule name: %s", name),
			pathErr, ruleErr)
		return
	}

	ingressName = strings.Join(parts[0:len(parts)-2], "-")
	return
}

// isRegularExpression determines whether the given string s is a non-trivial regular expression,
// i.e., it can potentially match other strings different than itself.
func isRegularExpression(s string) bool {
	return len(s) < len(regexp.QuoteMeta(s))
}
