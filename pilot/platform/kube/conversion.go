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
	"strings"

	"github.com/golang/protobuf/proto"

	"istio.io/manager/model"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"

	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"
)

const (
	// ServiceSuffix is the hostname suffix used by a Kubernetes service
	// TODO: make DNS suffix configurable
	ServiceSuffix = "svc.cluster.local"
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

func convertTags(obj v1.ObjectMeta) model.Tag {
	out := make(model.Tag)
	for k, v := range obj.Labels {
		out[k] = v
	}
	return out
}

func convertService(svc v1.Service) *model.Service {
	ports := make([]*model.Port, 0)
	addr := ""
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		addr = svc.Spec.ClusterIP
	}

	for _, port := range svc.Spec.Ports {
		ports = append(ports, &model.Port{
			Name:     port.Name,
			Port:     int(port.Port),
			Protocol: convertProtocol(port.Name, port.Protocol),
		})
	}

	return &model.Service{
		Hostname: fmt.Sprintf("%s.%s.%s", svc.Name, svc.Namespace, ServiceSuffix),
		Ports:    ports,
		Address:  addr,
	}
}

// parseHostname is the inverse of hostname pattern from convertService
func parseHostname(hostname string) (string, string, error) {
	prefix := strings.TrimSuffix(hostname, "."+ServiceSuffix)
	if len(prefix) >= len(hostname) {
		return "", "", fmt.Errorf("Missing hostname suffix: %q", hostname)
	}
	parts := strings.Split(prefix, ".")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("Incorrect number of hostname labels, expect 2, got %d, %q", len(parts), prefix)
	}
	return parts[0], parts[1], nil
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

// modelToKube translate Istio config to k8s config JSON
func modelToKube(km model.KindMap, k *model.Key, v proto.Message) (*Config, error) {
	if err := km.ValidateConfig(k, v); err != nil {
		return nil, err
	}
	kind := km[k.Kind]
	spec, err := kind.ToJSONMap(v)
	if err != nil {
		return nil, err
	}
	out := &Config{
		TypeMeta: meta_v1.TypeMeta{
			Kind: IstioKind,
		},
		Metadata: api.ObjectMeta{
			Name:      k.Kind + "-" + k.Name,
			Namespace: k.Namespace,
		},
		Spec: spec,
	}

	return out, nil
}
