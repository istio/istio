package cmd

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	openapiclient "k8s.io/client-go/openapi"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/validation"

	"istio.io/istio/pkg/kube"
)

type Factory struct {
	kube.PartialFactory
	full util.Factory
}

func (f Factory) NewBuilder() *resource.Builder {
	return f.full.NewBuilder()
}

func (f Factory) ClientForMapping(mapping *meta.RESTMapping) (resource.RESTClient, error) {
	return f.full.ClientForMapping(mapping)
}

func (f Factory) UnstructuredClientForMapping(mapping *meta.RESTMapping) (resource.RESTClient, error) {
	return f.full.UnstructuredClientForMapping(mapping)
}

func (f Factory) Validator(validationDirective string) (validation.Schema, error) {
	return f.full.Validator(validationDirective)
}

func (f Factory) OpenAPISchema() (openapi.Resources, error) {
	return f.full.OpenAPISchema()
}

func (f Factory) OpenAPIV3Client() (openapiclient.Client, error) {
	return f.full.OpenAPIV3Client()
}

var _ util.Factory = Factory{}

// MakeKubeFactory turns a partial kubetl factory from CLIClient into a full util.Factory
// This is done under istioctl/ to avoid excessive binary bloat in other packages; this pulls in around 10mb of
// dependencies.
var MakeKubeFactory = func(k kube.CLIClient) util.Factory {
	kf := k.UtilFactory()
	return Factory{
		PartialFactory: kf,
		full:           util.NewFactory(kf),
	}
}
