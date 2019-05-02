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

package generic

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/packagemanifest/install"
	packagemanifest "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/packagemanifest/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/provider"
	packagemanifeststorage "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/storage/packagemanifest"
)

var (
	// Scheme contains the types needed by the resource metrics API.
	Scheme = runtime.NewScheme()
	// Codecs is a codec factory for serving the resource metrics API.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	install.Install(Scheme)
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
}

// ProviderConfig holds the providers for packagemanifests.
type ProviderConfig struct {
	Provider provider.PackageManifestProvider
}

// BuildStorage constructs APIGroupInfo the packages.apps.redhat.com API group.
func BuildStorage(providers *ProviderConfig) genericapiserver.APIGroupInfo {
	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(packagemanifest.Group, Scheme, metav1.ParameterCodec, Codecs)

	packageManifestStorage := packagemanifeststorage.NewStorage(packagemanifest.Resource("packagemanifests"), providers.Provider)
	packageManifestResources := map[string]rest.Storage{
		"packagemanifests": packageManifestStorage,
	}
	apiGroupInfo.VersionedResourcesStorageMap[packagemanifest.Version] = packageManifestResources

	return apiGroupInfo
}

// InstallStorage builds the storage for the metrics.k8s.io API, and then installs it into the given API server.
func InstallStorage(providers *ProviderConfig, server *genericapiserver.GenericAPIServer) error {
	info := BuildStorage(providers)
	return server.InstallAPIGroup(&info)
}
