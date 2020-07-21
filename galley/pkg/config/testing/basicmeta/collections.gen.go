// GENERATED FILE -- DO NOT EDIT
//

package basicmeta

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
)

var (

	// Collection2 describes the collection collection2
	Collection2 = collection.Builder{
		Name:         "collection2",
		VariableName: "Collection2",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "testdata.istio.io",
			Kind:          "Kind1",
			Plural:        "Kind1s",
			Version:       "v1alpha1",
			Proto:         "google.protobuf.Struct",
			ProtoPackage:  "github.com/gogo/protobuf/types",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCollection1 describes the collection k8s/collection1
	K8SCollection1 = collection.Builder{
		Name:         "k8s/collection1",
		VariableName: "K8SCollection1",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "testdata.istio.io",
			Kind:          "Kind1",
			Plural:        "Kind1s",
			Version:       "v1alpha1",
			Proto:         "google.protobuf.Struct",
			ProtoPackage:  "github.com/gogo/protobuf/types",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

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

	// Pilot contains only collections used by Pilot.
	Pilot = collection.NewSchemasBuilder().
		Build()

	// PilotServiceApi contains only collections used by Pilot, including experimental Service Api.
	PilotServiceApi = collection.NewSchemasBuilder().
			Build()

	// Deprecated contains only collections used by that will soon be used by nothing.
	Deprecated = collection.NewSchemasBuilder().
			Build()
)
