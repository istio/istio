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

package tpr

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
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
)

// configKey assigns k8s TPR name to Istio config
func configKey(typ, key string) string {
	switch typ {
	case model.RouteRule, model.IngressRule:
		return typ + "-" + key
	case model.DestinationPolicy:
		// TODO: special key encoding for long hostnames-based keys
		parts := strings.Split(key, ".")
		return typ + "-" + strings.Replace(parts[0], "-", "--", -1) +
			"-" + strings.Replace(parts[1], "-", "--", -1)
	}
	return key
}

// modelToKube translates Istio config to k8s config JSON
func modelToKube(schema model.ProtoSchema, namespace string, config proto.Message) (*Config, error) {
	spec, err := schema.ToJSONMap(config)
	if err != nil {
		return nil, err
	}
	out := &Config{
		TypeMeta: meta_v1.TypeMeta{
			Kind: IstioKind,
		},
		Metadata: meta_v1.ObjectMeta{
			Name:      configKey(schema.Type, schema.Key(config)),
			Namespace: namespace,
		},
		Spec: spec,
	}

	return out, nil
}

// convertConfig extracts Istio config data from k8s TPRs
func (cl *Client) convertConfig(item *Config) (model.Config, error) {
	for _, schema := range cl.ConfigDescriptor() {
		if strings.HasPrefix(item.Metadata.Name, schema.Type) {
			data, err := schema.FromJSONMap(item.Spec)
			if err != nil {
				return model.Config{}, err
			}
			return model.Config{
				Type:     schema.Type,
				Key:      schema.Key(data),
				Revision: item.Metadata.ResourceVersion,
				Content:  data,
			}, nil
		}
	}
	return model.Config{}, fmt.Errorf("missing schema")
}

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

func convertIngress(ingress v1beta1.Ingress, domainSuffix string) map[string]proto.Message {
	messages := make(map[string]proto.Message)

	keyOf := func(ruleNum, pathNum int) string {
		return encodeIngressRuleName(ingress.Name, ingress.Namespace, ruleNum, pathNum)
	}

	tls := ""
	if len(ingress.Spec.TLS) > 0 {
		// due to lack of listener SNI in the proxy, we only support a single secret and ignore secret hosts
		secret := ingress.Spec.TLS[0]
		tls = fmt.Sprintf("%s.%s", secret.SecretName, ingress.Namespace)
	}

	if ingress.Spec.Backend != nil {
		key := keyOf(0, 0)
		messages[key] = createIngressRule(key, "", "", ingress.Namespace, domainSuffix, *ingress.Spec.Backend, tls)
	}

	for i, rule := range ingress.Spec.Rules {
		for j, path := range rule.HTTP.Paths {
			key := keyOf(i+1, j+1)
			messages[key] = createIngressRule(key, rule.Host, path.Path, ingress.Namespace,
				domainSuffix, path.Backend, tls)
		}
	}

	return messages
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
	return fmt.Sprintf("%s/%s/%d/%d", ingressNamespace, ingressName, ruleNum, pathNum)
}

// decodeIngressRuleName decodes an ingress rule name previously encoded with encodeIngressRuleName.
func decodeIngressRuleName(name string) (ingressName, ingressNamespace string, ruleNum, pathNum int, err error) {
	parts := strings.Split(name, "/")
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
func isRegularExpression(s string) bool {
	return len(s) < len(regexp.QuoteMeta(s))
}

// serviceHostname produces FQDN for a k8s service
func serviceHostname(name, namespace, domainSuffix string) string {
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, domainSuffix)
}
