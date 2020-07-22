// GENERATED FILE -- DO NOT EDIT
//

package k8smeta

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
)

var (

	// K8SAppsV1Deployments describes the collection k8s/apps/v1/deployments
	K8SAppsV1Deployments = collection.Builder{
		Name:         "k8s/apps/v1/deployments",
		VariableName: "K8SAppsV1Deployments",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "apps",
			Kind:          "Deployment",
			Plural:        "deployments",
			Version:       "v1",
			Proto:         "k8s.io.api.apps.v1.Deployment",
			ProtoPackage:  "k8s.io/api/apps/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Endpoints describes the collection k8s/core/v1/endpoints
	K8SCoreV1Endpoints = collection.Builder{
		Name:         "k8s/core/v1/endpoints",
		VariableName: "K8SCoreV1Endpoints",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Endpoints",
			Plural:        "endpoints",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.Endpoints",
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Namespaces describes the collection k8s/core/v1/namespaces
	K8SCoreV1Namespaces = collection.Builder{
		Name:         "k8s/core/v1/namespaces",
		VariableName: "K8SCoreV1Namespaces",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Namespace",
			Plural:        "namespaces",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.NamespaceSpec",
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Nodes describes the collection k8s/core/v1/nodes
	K8SCoreV1Nodes = collection.Builder{
		Name:         "k8s/core/v1/nodes",
		VariableName: "K8SCoreV1Nodes",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Node",
			Plural:        "nodes",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.NodeSpec",
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Pods describes the collection k8s/core/v1/pods
	K8SCoreV1Pods = collection.Builder{
		Name:         "k8s/core/v1/pods",
		VariableName: "K8SCoreV1Pods",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Pod",
			Plural:        "pods",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.Pod",
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Services describes the collection k8s/core/v1/services
	K8SCoreV1Services = collection.Builder{
		Name:         "k8s/core/v1/services",
		VariableName: "K8SCoreV1Services",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Service",
			Plural:        "services",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.ServiceSpec",
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SExtensionsV1Beta1Ingresses describes the collection
	// k8s/extensions/v1beta1/ingresses
	K8SExtensionsV1Beta1Ingresses = collection.Builder{
		Name:         "k8s/extensions/v1beta1/ingresses",
		VariableName: "K8SExtensionsV1Beta1Ingresses",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "extensions",
			Kind:          "Ingress",
			Plural:        "ingresses",
			Version:       "v1beta1",
			Proto:         "k8s.io.api.extensions.v1beta1.IngressSpec",
			ProtoPackage:  "k8s.io/api/extensions/v1beta1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
		MustAdd(K8SAppsV1Deployments).
		MustAdd(K8SCoreV1Endpoints).
		MustAdd(K8SCoreV1Namespaces).
		MustAdd(K8SCoreV1Nodes).
		MustAdd(K8SCoreV1Pods).
		MustAdd(K8SCoreV1Services).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
		Build()

	// Istio contains only Istio collections.
	Istio = collection.NewSchemasBuilder().
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
		MustAdd(K8SAppsV1Deployments).
		MustAdd(K8SCoreV1Endpoints).
		MustAdd(K8SCoreV1Namespaces).
		MustAdd(K8SCoreV1Nodes).
		MustAdd(K8SCoreV1Pods).
		MustAdd(K8SCoreV1Services).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
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
