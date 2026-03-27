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
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// EcdsGenerator generates ECDS configuration.
type EcdsGenerator struct {
	ConfigGenerator  core.ConfigGenerator
	secretController credscontroller.MulticlusterController
}

var _ model.XdsResourceGenerator = &EcdsGenerator{}

func ecdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	// Only push if config updates is triggered by EnvoyFilter, WasmPlugin, or Secret.
	for config := range req.ConfigsUpdated {
		switch config.Kind {
		case kind.EnvoyFilter:
			return true
		case kind.WasmPlugin:
			return true
		case kind.Secret:
			return true
		}
	}
	return false
}

// onlyReferencedConfigsUpdated indicates whether the PushRequest
// has ONLY referenced resource in ConfigUpdates. For example ONLY
// secret is updated that may be referred by Wasm Plugin.
func onlyReferencedConfigsUpdated(req *model.PushRequest) bool {
	referencedConfigUpdated := false
	for config := range req.ConfigsUpdated {
		switch config.Kind {
		case kind.EnvoyFilter:
			return false
		case kind.WasmPlugin:
			return false
		case kind.Secret:
			referencedConfigUpdated = true
		}
	}
	return referencedConfigUpdated
}

// Generate returns ECDS resources for a given proxy.
func (e *EcdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ecdsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}

	wasmSecrets := referencedSecrets(proxy, req.Push, w.ResourceNames)

	// When referenced configs are ONLY updated (like secret update), we should push
	// if the referenced config is relevant for ECDS. A secret update is relevant
	// only if it is referred via WASM plugin.
	if onlyReferencedConfigsUpdated(req) {
		updatedSecrets := model.ConfigsOfKind(req.ConfigsUpdated, kind.Secret)
		needsPush := false
		for _, sr := range wasmSecrets {
			if _, found := updatedSecrets[model.ConfigKey{Kind: kind.Secret, Name: sr.Name, Namespace: sr.Namespace}]; found {
				needsPush = true
				break
			}
		}
		if !needsPush {
			return nil, model.DefaultXdsLogDetails, nil
		}
	}

	var secrets map[string][]byte
	if len(wasmSecrets) > 0 {
		// Generate the pull secrets first, which will be used when populating the extension config.
		if e.secretController != nil {
			var err error
			secretController, err := e.secretController.ForCluster(proxy.Metadata.ClusterID)
			if err != nil {
				log.Warnf("proxy %s is from an unknown cluster, cannot retrieve certificates for Wasm image pull: %v", proxy.ID, err)
				return nil, model.DefaultXdsLogDetails, nil
			}
			// Inserts Wasm pull secrets in ECDS response, which will be used at xds proxy for image pull.
			// Before forwarding to Envoy, xds proxy will remove the secret from ECDS response.
			secrets = e.GeneratePullSecrets(proxy, wasmSecrets, secretController)
		}
	}

	ec := e.ConfigGenerator.BuildExtensionConfiguration(proxy, req.Push, w.ResourceNames.UnsortedList(), secrets)

	if ec == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}

	resources := make(model.Resources, 0, len(ec))
	for _, c := range ec {
		resources = append(resources, &discovery.Resource{
			Name:     c.Name,
			Resource: protoconv.MessageToAny(c),
		})
	}

	return resources, model.DefaultXdsLogDetails, nil
}

func (e *EcdsGenerator) GeneratePullSecrets(proxy *model.Proxy, secretResources []SecretResource,
	secretController credscontroller.Controller,
) map[string][]byte {
	if proxy.VerifiedIdentity == nil {
		log.Warnf("proxy %s is not authorized to receive secret. Ensure you are connecting over TLS port and are authenticated.", proxy.ID)
		return nil
	}

	results := make(map[string][]byte)
	for _, sr := range secretResources {
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

func referencedSecrets(proxy *model.Proxy, push *model.PushContext, watched sets.String) []SecretResource {
	// The requirement for the Wasm pull secret:
	// * Wasm pull secrets must be of type `kubernetes.io/dockerconfigjson`.
	// * Secret are referenced by a WasmPlugin which applies to this proxy.
	// TODO: we get the WasmPlugins here to get the secrets reference in order to decide whether ECDS push is needed,
	//       and we will get it again at extension config build. Avoid getting it twice if this becomes a problem.
	wasmPlugins := push.WasmPlugins(proxy)
	referencedSecrets := sets.String{}
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
		return SecretResource{}, fmt.Errorf("malformed Wasm pull secret resource name %v", resourceName)
	}
	res := strings.TrimPrefix(resourceName, credentials.KubernetesSecretTypeURI)
	sep := "/"
	split := strings.Split(res, sep)
	if len(split) != 2 {
		return SecretResource{}, fmt.Errorf("malformed Wasm pull secret resource name %v", resourceName)
	}
	return SecretResource{
		SecretResource: credentials.SecretResource{
			ResourceType: credentials.KubernetesSecretType,
			Name:         split[1],
			Namespace:    split[0],
			ResourceName: resourceName,
			Cluster:      proxyCluster,
		},
	}, nil
}
