package crdclient

import (
	"context"
	"fmt"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metadatafake "k8s.io/client-go/metadata/fake"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

func CreateCRD(t test.Failer, client kube.Client, r resource.Schema) {
	t.Helper()
	crd := &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", r.Plural(), r.Group()),
		},
	}
	if _, err := client.Ext().ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	// Metadata client fake is not kept in sync, so if using a fake clinet update that as well
	fmc, ok := client.Metadata().(*metadatafake.FakeMetadataClient)
	if !ok {
		return
	}
	fmg := fmc.Resource(collections.K8SApiextensionsK8SIoV1Customresourcedefinitions.Resource().GroupVersionResource())
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
