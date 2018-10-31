// Copyright 2018 Istio Authors
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

package converter

import (
	json2 "encoding/json"
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/galley/pkg/kube/converter/legacy"
	"istio.io/istio/galley/pkg/runtime/resource"
)

var scope = log.RegisterScope("kube-converter", "Kubernetes conversion related packages", 0)

// Fn is a conversion function that converts the given unstructured CRD into the destination Resource.
type Fn func(cfg *Config, destination resource.Info, name resource.FullName, u *unstructured.Unstructured) ([]Entry, error)

// Entry is a single converted entry.
type Entry struct {
	Key          resource.FullName
	CreationTime time.Time
	Resource     proto.Message
}

var converters = map[string]Fn{
	"identity": identity,
	"nil":      nilConverter,
	"legacy-mixer-resource": legacyMixerResource,
	"auth-policy-resource":  authPolicyResource,
	"kube-ingress-resource": kubeIngressResource,
}

// Get returns the named converter function, or panics if it is not found.
func Get(name string) Fn {
	fn, found := converters[name]
	if !found {
		panic(fmt.Sprintf("converter.Get: converter not found: %s", name))
	}

	return fn
}

func identity(_ *Config, destination resource.Info, name resource.FullName, u *unstructured.Unstructured) ([]Entry, error) {
	p, err := toProto(destination, u.Object["spec"])
	if err != nil {
		return nil, err
	}

	e := Entry{
		Key:          name,
		CreationTime: u.GetCreationTimestamp().Time,
		Resource:     p,
	}

	return []Entry{e}, nil
}

func nilConverter(_ *Config, _ resource.Info, _ resource.FullName, _ *unstructured.Unstructured) ([]Entry, error) {
	return nil, nil
}

func legacyMixerResource(_ *Config, _ resource.Info, name resource.FullName, u *unstructured.Unstructured) ([]Entry, error) {
	spec := u.Object["spec"]
	s := &types.Struct{}
	if err := toproto(s, spec); err != nil {
		return nil, err
	}

	newName := resource.FullNameFromNamespaceAndName(u.GetKind(), name.String())

	e := Entry{
		Key:          newName,
		CreationTime: u.GetCreationTimestamp().Time,
		Resource: &legacy.LegacyMixerResource{
			Name:     name.String(),
			Kind:     u.GetKind(),
			Contents: s,
		},
	}

	return []Entry{e}, nil
}

func authPolicyResource(_ *Config, destination resource.Info, name resource.FullName, u *unstructured.Unstructured) ([]Entry, error) {
	p, err := toProto(destination, u.Object["spec"])
	if err != nil {
		return nil, err
	}

	policy, ok := p.(*authn.Policy)
	if !ok {
		return nil, fmt.Errorf("object is not of type %v", destination.TypeURL)
	}

	// The pilot authentication plugin's config handling allows the mtls
	// peer method object value to be nil. See pilot/pkg/networking/plugin/authn/authentication.go#L68
	//
	// For example,
	//
	//     metadata:
	//       name: d-ports-mtls-enabled
	//     spec:
	//       targets:
	//       - name: d
	//         ports:
	//         - number: 80
	//       peers:
	//       - mtls:
	//
	// This translates to the following in-memory representation:
	//
	//     policy := &authn.Policy{
	//       Peers: []*authn.PeerAuthenticationMethod{{
	//         &authn.PeerAuthenticationMethod_Mtls{},
	//       }},
	//     }
	//
	// The PeerAuthenticationMethod_Mtls object with nil field is lost when
	// the proto is re-encoded for transport via MCP. As a workaround, fill
	// in the missing field value which is functionality equivalent.
	for _, peer := range policy.Peers {
		if mtls, ok := peer.Params.(*authn.PeerAuthenticationMethod_Mtls); ok && mtls.Mtls == nil {
			mtls.Mtls = &authn.MutualTls{}
		}
	}

	e := Entry{
		Key:          name,
		CreationTime: u.GetCreationTimestamp().Time,
		Resource:     p,
	}

	return []Entry{e}, nil
}

func kubeIngressResource(cfg *Config, _ resource.Info, name resource.FullName, u *unstructured.Unstructured) ([]Entry, error) {
	json, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var p extensions.Ingress
	if err = json2.Unmarshal(json, &p); err != nil {
		return nil, err
	}

	if !shouldProcessIngress(cfg, &p) {
		return nil, nil
	}

	e := Entry{
		Key:          name,
		CreationTime: u.GetCreationTimestamp().Time,
		Resource:     &p.Spec,
	}

	return []Entry{e}, nil
}

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func shouldProcessIngress(cfg *Config, i *extensions.Ingress) bool {
	class, exists := "", false
	if i.Annotations != nil {
		class, exists = i.Annotations[kube.IngressClassAnnotation]
	}

	mesh := cfg.Mesh.Get()
	switch mesh.IngressControllerMode {
	case meshconfig.MeshConfig_OFF:
		scope.Debugf("Skipping ingress due to Ingress Controller Mode OFF (%s/%s)", i.Namespace, i.Name)
		return false
	case meshconfig.MeshConfig_STRICT:
		result := exists && class == mesh.IngressClass
		scope.Debugf("Checking ingress class w/ Strict (%s/%s): %v", i.Namespace, i.Name, result)
		return result
	case meshconfig.MeshConfig_DEFAULT:
		result := !exists || class == mesh.IngressClass
		scope.Debugf("Checking ingress class w/ Default (%s/%s): %v", i.Namespace, i.Name, result)
		return result
	default:
		scope.Warnf("invalid i synchronization mode: %v", mesh.IngressControllerMode)
		return false
	}
}
