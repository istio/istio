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
	"testing"

	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress/core/pkg/ingress/annotations/class"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
)

func makeAnnotatedIngress(annotation string) *extensions.Ingress {
	if annotation == "" {
		return &extensions.Ingress{}
	}

	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				kube.IngressClassAnnotation: annotation,
			},
		},
	}
}

// TestConvertIngressControllerMode ensures that ingress controller mode is converted to the k8s ingress status syncer's
// representation correctly.
func TestConvertIngressControllerMode(t *testing.T) {
	cases := []struct {
		Mode       meshconfig.MeshConfig_IngressControllerMode
		Annotation string
		Ignore     bool
	}{
		{
			Mode:       meshconfig.MeshConfig_DEFAULT,
			Annotation: "",
			Ignore:     true,
		},
		{
			Mode:       meshconfig.MeshConfig_DEFAULT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       meshconfig.MeshConfig_DEFAULT,
			Annotation: "nginx",
			Ignore:     false,
		},
		{
			Mode:       meshconfig.MeshConfig_STRICT,
			Annotation: "",
			Ignore:     false,
		},
		{
			Mode:       meshconfig.MeshConfig_STRICT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       meshconfig.MeshConfig_STRICT,
			Annotation: "nginx",
			Ignore:     false,
		},
	}

	for _, c := range cases {
		ingressClass, defaultIngressClass := convertIngressControllerMode(c.Mode, "istio")

		ing := makeAnnotatedIngress(c.Annotation)
		if ignore := class.IsValid(ing, ingressClass, defaultIngressClass); ignore != c.Ignore {
			t.Errorf("convertIngressControllerMode(%q, %q), with Ingress annotation %q => "+
				"Got ignore %v, want %v", c.Mode, "istio", c.Annotation, c.Ignore, ignore)
		}
	}
}
