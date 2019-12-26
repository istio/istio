// GENERATED FILE -- DO NOT EDIT
//

package basicmeta

import (
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/meta/schema/resource"
	"istio.io/istio/pkg/config/validation"
)

var (

	// Collection2 describes the collection collection2
	Collection2 = collection.Schema{
		Name:     collection.NewName("collection2"),
		Disabled: false,
		Schema: resource.Schema{
			Group:         "testdata.istio.io",
			Kind:          "Kind1",
			Plural:        "Kind1s",
			Version:       "v1alpha1",
			Proto:         "google.protobuf.Struct",
			ProtoPackage:  "github.com/gogo/protobuf/types",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		},
	}

	// K8SCollection1 describes the collection k8s/collection1
	K8SCollection1 = collection.Schema{
		Name:     collection.NewName("k8s/collection1"),
		Disabled: false,
		Schema: resource.Schema{
			Group:         "testdata.istio.io",
			Kind:          "Kind1",
			Plural:        "Kind1s",
			Version:       "v1alpha1",
			Proto:         "google.protobuf.Struct",
			ProtoPackage:  "github.com/gogo/protobuf/types",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		},
	}

	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
		MustAdd(Collection2).
		MustAdd(K8SCollection1).
		Build()

	// Istio contains only Istio collections.
	Istio = collection.NewSchemasBuilder().
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
		MustAdd(K8SCollection1).
		Build()
)
