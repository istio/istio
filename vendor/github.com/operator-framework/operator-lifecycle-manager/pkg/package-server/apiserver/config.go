// Copyright 2018 The Kubernetes Authors.
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

package apiserver

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"

	generatedopenapi "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/openapi"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/packagemanifest/install"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apiserver/generic"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/version"
)

var (
	Scheme = runtime.NewScheme()
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	install.Install(Scheme)

	// we need to add the options to empty v1
	// TODO fix the server code to avoid this
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})

	// TODO: keep the generic API server from wanting this
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

// Config contains configuration for launching an instance of metrics-server.
type Config struct {
	GenericConfig  *genericapiserver.Config
	ProviderConfig generic.ProviderConfig
}

type completedConfig struct {
	genericapiserver.CompletedConfig
	ProviderConfig *generic.ProviderConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (c *Config) Complete(informers informers.SharedInformerFactory) completedConfig {
	c.GenericConfig.Version = version.VersionInfo()

	// enable OpenAPI schemas
	c.GenericConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(generatedopenapi.GetOpenAPIDefinitions, openapinamer.NewDefinitionNamer(generic.Scheme))
	c.GenericConfig.OpenAPIConfig.Info.Title = "Package API server"
	c.GenericConfig.OpenAPIConfig.Info.Version = strings.Split(c.GenericConfig.Version.String(), "-")[0]
	c.GenericConfig.SwaggerConfig = genericapiserver.DefaultSwaggerConfig()

	return completedConfig{
		CompletedConfig: c.GenericConfig.Complete(informers),
		ProviderConfig:  &c.ProviderConfig,
	}
}

type PackageManifestServer struct {
	*genericapiserver.GenericAPIServer
}

// New returns a new instance of MetricsServer from the given config.
func (c completedConfig) New() (*PackageManifestServer, error) {
	genericServer, err := c.CompletedConfig.New("packagemanifest-server", genericapiserver.NewEmptyDelegate()) // completion is done in Complete, no need for a second time
	if err != nil {
		return nil, err
	}

	if err := generic.InstallStorage(c.ProviderConfig, genericServer); err != nil {
		return nil, err
	}

	return &PackageManifestServer{
		GenericAPIServer: genericServer,
	}, nil
}
