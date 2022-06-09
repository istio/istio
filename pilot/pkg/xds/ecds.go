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

package xds

import (
	"fmt"
	"strings"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	credscontroller "istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

// EcdsGenerator generates ECDS configuration.
type EcdsGenerator struct {
	Server           *DiscoveryServer
	secretController credscontroller.MulticlusterController
}

var _ model.XdsResourceGenerator = &EcdsGenerator{}

func ecdsNeedsPush(req *model.PushRequest) bool {
	if req == nil {
		return true
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	// Only push if config updates is triggered by EnvoyFilter, WasmPlugin, or Secret.
	for config := range req.ConfigsUpdated {
		switch config.Kind {
		case gvk.EnvoyFilter:
			return true
		case gvk.WasmPlugin:
			return true
		case gvk.Secret:
			return true
		}
	}
	return false
}

// Generate returns ECDS resources for a given proxy.
func (e *EcdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ecdsNeedsPush(req) {
		return nil, model.DefaultXdsLogDetails, nil
	}

	secretResources := referencedSecrets(proxy, req.Push, w.ResourceNames)
	var updatedSecrets map[model.ConfigKey]struct{}
	// Check if the secret updates is relevant to Wasm image pull. If not relevant, skip pushing ECDS.
	if !model.ConfigsHaveKind(req.ConfigsUpdated, gvk.WasmPlugin) && !model.ConfigsHaveKind(req.ConfigsUpdated, gvk.EnvoyFilter) &&
		model.ConfigsHaveKind(req.ConfigsUpdated, gvk.Secret) {
		// Get the updated secrets
		updatedSecrets = model.ConfigsOfKind(req.ConfigsUpdated, gvk.Secret)
		needsPush := false
		for _, sr := range secretResources {
			if _, found := updatedSecrets[model.ConfigKey{Kind: gvk.Secret, Name: sr.Name, Namespace: sr.Namespace}]; found {
				needsPush = true
				break
			}
		}
		if !needsPush {
			return nil, model.DefaultXdsLogDetails, nil
		}
	}

	var secrets map[string][]byte
	if len(secretResources) > 0 {
		// Generate the pull secrets first, which will be used when populating the extension config.
		var secretController credscontroller.Controller
		if e.secretController != nil {
			var err error
			secretController, err = e.secretController.ForCluster(proxy.Metadata.ClusterID)
			if err != nil {
				log.Warnf("proxy %s is from an unknown cluster, cannot retrieve certificates for Wasm image pull: %v", proxy.ID, err)
				return nil, model.DefaultXdsLogDetails, nil
			}
		}
		// Inserts Wasm pull secrets in ECDS response, which will be used at xds proxy for image pull.
		// Before forwarding to Envoy, xds proxy will remove the secret from ECDS response.
		secrets = e.GeneratePullSecrets(proxy, updatedSecrets, secretResources, secretController, req)
	}

	ec := e.Server.ConfigGenerator.BuildExtensionConfiguration(proxy, req.Push, w.ResourceNames, secrets)

	if ec == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}

	resources := make(model.Resources, 0, len(ec))
	for _, c := range ec {
		resources = append(resources, &discovery.Resource{
			Name:     c.Name,
			Resource: util.MessageToAny(c),
		})
	}

	return resources, model.DefaultXdsLogDetails, nil
}

func (e *EcdsGenerator) GeneratePullSecrets(proxy *model.Proxy, updatedSecrets map[model.ConfigKey]struct{}, secretResources []SecretResource,
	secretController credscontroller.Controller, req *model.PushRequest,
) map[string][]byte {
	if proxy.VerifiedIdentity == nil {
		log.Warnf("proxy %s is not authorized to receive secret. Ensure you are connecting over TLS port and are authenticated.", proxy.ID)
		return nil
	}

	results := make(map[string][]byte)
	for _, sr := range secretResources {
		if updatedSecrets != nil {
			if _, found := updatedSecrets[model.ConfigKey{Kind: gvk.Secret, Name: sr.Name, Namespace: sr.Namespace}]; !found {
				// This is an incremental update, filter out credscontroller that are not updated.
				continue
			}
		}

		cred, err := secretController.GetDockerCredential(sr.Name, sr.Namespace)
		if err != nil {
			log.Warnf("Failed to fetch docker credential %s: %v", sr.ResourceName, err)
		} else {
			results[sr.ResourceName] = cred
		}
	}
	return results
}

func (e *EcdsGenerator) SetCredController(creds credscontroller.MulticlusterController) {
	e.secretController = creds
}

func referencedSecrets(proxy *model.Proxy, push *model.PushContext, resourceNames []string) []SecretResource {
	// The requirement for the Wasm pull secret:
	// * Wasm pull secrets must be of type `kubernetes.io/dockerconfigjson`.
	// * Secret are referenced by a WasmPlugin which applies to this proxy.
	// TODO: we get the WasmPlugins here to get the secrets reference in order to decide whether ECDS push is needed,
	//       and we will get it again at extension config build. Avoid getting it twice if this becomes a problem.
	watched := sets.New(resourceNames...)
	wasmPlugins := push.WasmPlugins(proxy)
	referencedSecrets := sets.Set{}
	for _, wps := range wasmPlugins {
		for _, wp := range wps {
			if watched.Contains(wp.ResourceName) && wp.ImagePullSecret != "" {
				referencedSecrets.Insert(wp.ImagePullSecret)
			}
		}
	}
	var filtered []SecretResource
	for rn := range referencedSecrets {
		sr, err := parseSecretName(rn, proxy.Metadata.ClusterID)
		if err != nil {
			log.Warnf("Failed to parse secret resource name %v: %v", rn, err)
			continue
		}
		filtered = append(filtered, sr)
	}

	return filtered
}

// parseSecretName parses secret resource name from WasmPlugin env variable.
// See toSecretResourceName at model/extensions.go about how secret resource name is generated.
func parseSecretName(resourceName string, proxyCluster cluster.ID) (SecretResource, error) {
	// The secret resource name must be formatted as kubernetes://secret-namespace/secret-name.
	if !strings.HasPrefix(resourceName, credentials.KubernetesSecretTypeURI) {
		return SecretResource{}, fmt.Errorf("misformed Wasm pull secret resource name %v", resourceName)
	}
	res := strings.TrimPrefix(resourceName, credentials.KubernetesSecretTypeURI)
	sep := "/"
	split := strings.Split(res, sep)
	if len(split) != 2 {
		return SecretResource{}, fmt.Errorf("misformed Wasm pull secret resource name %v", resourceName)
	}
	return SecretResource{
		SecretResource: credentials.SecretResource{
			Type:         credentials.KubernetesSecretType,
			Name:         split[1],
			Namespace:    split[0],
			ResourceName: resourceName,
			Cluster:      proxyCluster,
		},
	}, nil
}
