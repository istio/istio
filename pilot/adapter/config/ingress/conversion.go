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

package ingress

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
)

func convertIngress(ingress v1beta1.Ingress, domainSuffix string) map[string]*proxyconfig.IngressRule {
	out := make(map[string]*proxyconfig.IngressRule)
	tls := ""

	if len(ingress.Spec.TLS) > 0 {
		// due to lack of listener SNI in the proxy, we only support a single secret and ignore secret hosts
		if len(ingress.Spec.TLS) > 1 {
			glog.Warningf("ingress %s requires several TLS secrets which is not supported by envoy!", ingress.Name)
		}
		secret := ingress.Spec.TLS[0]
		tls = fmt.Sprintf("%s.%s", secret.SecretName, ingress.Namespace)
	}

	if ingress.Spec.Backend != nil {
		key := encodeIngressRuleName(ingress.Name, ingress.Namespace, 0, 0)
		ingressRule := createIngressRule(key, "", "", ingress.Namespace, domainSuffix, *ingress.Spec.Backend, tls)
		out[model.IngressRule.Key(ingressRule)] = ingressRule
	}

	for i, rule := range ingress.Spec.Rules {
		for j, path := range rule.HTTP.Paths {
			key := encodeIngressRuleName(ingress.Name, ingress.Namespace, i+1, j+1)
			ingressRule := createIngressRule(key, rule.Host, path.Path, ingress.Namespace,
				domainSuffix, path.Backend, tls)
			out[model.IngressRule.Key(ingressRule)] = ingressRule
		}
	}

	return out
}

func createIngressRule(name, host, path, namespace, domainSuffix string,
	backend v1beta1.IngressBackend, tlsSecret string) *proxyconfig.IngressRule {
	rule := &proxyconfig.IngressRule{
		Name:        name,
		Destination: serviceHostname(backend.ServiceName, namespace, domainSuffix),
		TlsSecret:   tlsSecret,
		Match: &proxyconfig.MatchCondition{
			HttpHeaders: make(map[string]*proxyconfig.StringMatch, 2),
		},
	}
	switch backend.ServicePort.Type {
	case intstr.Int:
		rule.DestinationServicePort = &proxyconfig.IngressRule_DestinationPort{
			DestinationPort: int32(backend.ServicePort.IntValue()),
		}
	case intstr.String:
		rule.DestinationServicePort = &proxyconfig.IngressRule_DestinationPortName{
			DestinationPortName: backend.ServicePort.String(),
		}
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
func encodeIngressRuleName(ingressName, ingressNamespace string, ruleNum, pathNum int) string {
	return fmt.Sprintf("%s.%s.%d.%d", ingressNamespace, ingressName, ruleNum, pathNum)
}

// decodeIngressRuleName decodes an ingress rule name previously encoded with encodeIngressRuleName.
func decodeIngressRuleName(name string) (ingressName, ingressNamespace string, ruleNum, pathNum int, err error) {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		err = fmt.Errorf("could not decode string into ingress rule name: %s", name)
		return
	}

	ingressNamespace = parts[0]
	ingressName = parts[1]
	ruleNum, ruleErr := strconv.Atoi(parts[2])
	pathNum, pathErr := strconv.Atoi(parts[3])

	if pathErr != nil || ruleErr != nil {
		err = multierror.Append(
			fmt.Errorf("could not decode string into ingress rule name: %s", name),
			pathErr, ruleErr)
		return
	}

	return
}

// isRegularExpression determines whether the given string s is a non-trivial regular expression,
// i.e., it can potentially match other strings different than itself.
// TODO: warning that Envoy regex language is not 1-1 with golang's regex language!
func isRegularExpression(s string) bool {
	return len(s) < len(regexp.QuoteMeta(s))
}

// serviceHostname produces FQDN for a k8s service
func serviceHostname(name, namespace, domainSuffix string) string {
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, domainSuffix)
}

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func shouldProcessIngress(mesh *proxyconfig.ProxyMeshConfig, ingress *v1beta1.Ingress) bool {
	class, exists := "", false
	if ingress.Annotations != nil {
		class, exists = ingress.Annotations[kube.IngressClassAnnotation]
	}

	switch mesh.IngressControllerMode {
	case proxyconfig.ProxyMeshConfig_OFF:
		return false
	case proxyconfig.ProxyMeshConfig_STRICT:
		return exists && class == mesh.IngressClass
	case proxyconfig.ProxyMeshConfig_DEFAULT:
		return !exists || class == mesh.IngressClass
	default:
		glog.Warningf("invalid ingress synchronization mode: %v", mesh.IngressControllerMode)
		return false
	}
}
