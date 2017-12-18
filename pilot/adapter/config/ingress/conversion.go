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
	"strconv"
	"strings"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshconfig "istio.io/api/mesh/v1alpha1"
	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/kube"
)

func convertIngress(ingress v1beta1.Ingress, domainSuffix string) []model.Config {
	out := make([]model.Config, 0)
	tls := ""

	if len(ingress.Spec.TLS) > 0 {
		// TODO(istio/istio/issues/1424): implement SNI
		if len(ingress.Spec.TLS) > 1 {
			glog.Warningf("ingress %s requires several TLS secrets but Envoy can only serve one", ingress.Name)
		}
		secret := ingress.Spec.TLS[0]
		tls = fmt.Sprintf("%s.%s", secret.SecretName, ingress.Namespace)
	}

	if ingress.Spec.Backend != nil {
		name := encodeIngressRuleName(ingress.Name, 0, 0)
		ingressRule := createIngressRule(name, "", "", domainSuffix, ingress, *ingress.Spec.Backend, tls)
		out = append(out, ingressRule)
	}

	for i, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			glog.Warningf("invalid ingress rule for host %q, no paths defined", rule.Host)
			continue
		}
		for j, path := range rule.HTTP.Paths {
			name := encodeIngressRuleName(ingress.Name, i+1, j+1)
			ingressRule := createIngressRule(name, rule.Host, path.Path,
				domainSuffix, ingress, path.Backend, tls)
			out = append(out, ingressRule)
		}
	}
	return out
}

func createIngressRule(name, host, path, domainSuffix string,
	ingress v1beta1.Ingress, backend v1beta1.IngressBackend, tlsSecret string) model.Config {
	rule := &routing.IngressRule{
		Destination: &routing.IstioService{
			Name: backend.ServiceName,
		},
		TlsSecret: tlsSecret,
		Match: &routing.MatchCondition{
			Request: &routing.MatchRequest{
				Headers: make(map[string]*routing.StringMatch, 2),
			},
		},
	}
	switch backend.ServicePort.Type {
	case intstr.Int:
		rule.DestinationServicePort = &routing.IngressRule_DestinationPort{
			DestinationPort: int32(backend.ServicePort.IntValue()),
		}
	case intstr.String:
		rule.DestinationServicePort = &routing.IngressRule_DestinationPortName{
			DestinationPortName: backend.ServicePort.String(),
		}
	}

	if host != "" {
		rule.Match.Request.Headers[model.HeaderAuthority] = &routing.StringMatch{
			MatchType: &routing.StringMatch_Exact{Exact: host},
		}
	}

	if path != "" {
		if strings.HasSuffix(path, ".*") {
			rule.Match.Request.Headers[model.HeaderURI] = &routing.StringMatch{
				MatchType: &routing.StringMatch_Prefix{Prefix: strings.TrimSuffix(path, ".*")},
			}
		} else {
			rule.Match.Request.Headers[model.HeaderURI] = &routing.StringMatch{
				MatchType: &routing.StringMatch_Exact{Exact: path},
			}
		}
	} else {
		rule.Match.Request.Headers[model.HeaderURI] = &routing.StringMatch{
			MatchType: &routing.StringMatch_Prefix{Prefix: "/"},
		}
	}

	return model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            model.IngressRule.Type,
			Name:            name,
			Namespace:       ingress.Namespace,
			Domain:          domainSuffix,
			Labels:          ingress.Labels,
			Annotations:     ingress.Annotations,
			ResourceVersion: ingress.ResourceVersion,
		},
		Spec: rule,
	}
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

	ingressName = strings.Join(parts[0:len(parts)-2], "-")
	ruleNum, ruleErr := strconv.Atoi(parts[len(parts)-2])
	pathNum, pathErr := strconv.Atoi(parts[len(parts)-1])

	if pathErr != nil || ruleErr != nil {
		err = multierror.Append(
			fmt.Errorf("could not decode string into ingress rule name: %s", name),
			pathErr, ruleErr)
		return
	}

	return
}

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func shouldProcessIngress(mesh *meshconfig.MeshConfig, ingress *v1beta1.Ingress) bool {
	class, exists := "", false
	if ingress.Annotations != nil {
		class, exists = ingress.Annotations[kube.IngressClassAnnotation]
	}

	switch mesh.IngressControllerMode {
	case meshconfig.MeshConfig_OFF:
		return false
	case meshconfig.MeshConfig_STRICT:
		return exists && class == mesh.IngressClass
	case meshconfig.MeshConfig_DEFAULT:
		return !exists || class == mesh.IngressClass
	default:
		glog.Warningf("invalid ingress synchronization mode: %v", mesh.IngressControllerMode)
		return false
	}
}
