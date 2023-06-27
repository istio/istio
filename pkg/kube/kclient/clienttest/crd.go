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

package clienttest

import (
	"fmt"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metadatafake "k8s.io/client-go/metadata/fake"

	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

func MakeCRD(t test.Failer, c kube.Client, g schema.GroupVersionResource) {
	t.Helper()
	crd := &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", g.Resource, g.Group),
		},
	}
	// Metadata client fake is not kept in sync, so if using a fake client update that as well
	fmc, ok := c.Metadata().(*metadatafake.FakeMetadataClient)
	if !ok {
		return
	}
	fmg := fmc.Resource(gvr.CustomResourceDefinition)
	fmd, ok := fmg.(metadatafake.MetadataClient)
	if !ok {
		return
	}
	if _, err := fmd.CreateFake(&metav1.PartialObjectMetadata{
		TypeMeta:   crd.TypeMeta,
		ObjectMeta: crd.ObjectMeta,
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}
