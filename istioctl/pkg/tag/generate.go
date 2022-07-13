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

package tag

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	admit_v1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/pkg/kube"
)

const (
	IstioTagLabel       = "istio.io/tag"
	DefaultRevisionName = "default"

	defaultChart            = "default"
	pilotDiscoveryChart     = "istio-control/istio-discovery"
	revisionTagTemplateName = "revision-tags.yaml"
	vwhTemplateName         = "validatingwebhook.yaml"

	istioInjectionWebhookSuffix = "sidecar-injector.istio.io"
)

// tagWebhookConfig holds config needed to render a tag webhook.
type tagWebhookConfig struct {
	Tag            string
	Revision       string
	URL            string
	Path           string
	CABundle       string
	IstioNamespace string
}

// GenerateOptions is the group of options needed to generate a tag webhook.
type GenerateOptions struct {
	// Tag is the name of the revision tag to generate.
	Tag string
	// Revision is the revision to associate the revision tag with.
	Revision string
	// WebhookName is an override for the mutating webhook name.
	WebhookName string
	// ManifestsPath specifies where the manifests to render the mutatingwebhook can be found.
	// TODO(Monkeyanator) once we stop using Helm templating remove this.
	ManifestsPath string
	// Generate determines whether we should just generate the webhooks without applying. This
	// applying is not done here, but we are looser with checks when doing generate.
	Generate bool
	// Overwrite removes analysis checks around existing webhooks.
	Overwrite bool
	// AutoInjectNamespaces controls, if the sidecars should be injected into all namespaces by default.
	AutoInjectNamespaces bool
}

// Generate generates the manifests for a revision tag pointed the given revision.
func Generate(ctx context.Context, client kube.ExtendedClient, opts *GenerateOptions, istioNS string) (string, error) {
	// abort if there exists a revision with the target tag name
	revWebhookCollisions, err := GetWebhooksWithRevision(ctx, client.Kube(), opts.Tag)
	if err != nil {
		return "", err
	}
	if !opts.Generate && !opts.Overwrite &&
		len(revWebhookCollisions) > 0 && opts.Tag != DefaultRevisionName {
		return "", fmt.Errorf("cannot create revision tag %q: found existing control plane revision with same name", opts.Tag)
	}

	// find canonical revision webhook to base our tag webhook off of
	revWebhooks, err := GetWebhooksWithRevision(ctx, client.Kube(), opts.Revision)
	if err != nil {
		return "", err
	}
	if len(revWebhooks) == 0 {
		return "", fmt.Errorf("cannot modify tag: cannot find MutatingWebhookConfiguration with revision %q", opts.Revision)
	}
	if len(revWebhooks) > 1 {
		return "", fmt.Errorf("cannot modify tag: found multiple canonical webhooks with revision %q", opts.Revision)
	}

	whs, err := GetWebhooksWithTag(ctx, client.Kube(), opts.Tag)
	if err != nil {
		return "", err
	}
	if len(whs) > 0 && !opts.Overwrite {
		return "", fmt.Errorf("revision tag %q already exists, and --overwrite is false", opts.Tag)
	}

	tagWhConfig, err := tagWebhookConfigFromCanonicalWebhook(revWebhooks[0], opts.Tag, istioNS)
	if err != nil {
		return "", fmt.Errorf("failed to create tag webhook config: %w", err)
	}
	tagWhYAML, err := generateMutatingWebhook(tagWhConfig, opts.WebhookName, opts.ManifestsPath, opts.AutoInjectNamespaces)
	if err != nil {
		return "", fmt.Errorf("failed to create tag webhook: %w", err)
	}

	if opts.Tag == DefaultRevisionName {
		if !opts.Generate {
			// deactivate other istio-injection=enabled injectors if using default revisions.
			err := DeactivateIstioInjectionWebhook(ctx, client.Kube())
			if err != nil {
				return "", fmt.Errorf("failed deactivating existing default revision: %w", err)
			}
			// delete deprecated validating webhook configuration if it exists.
			err = DeleteDeprecatedValidator(ctx, client.Kube())
			if err != nil {
				return "", fmt.Errorf("failed removing deprecated validating webhook: %w", err)
			}
		}

		// TODO(Monkeyanator) should extract the validationURL from revision's validating webhook here. However,
		// to ease complexity when pointing default to revision without per-revision validating webhook,
		// instead grab the endpoint information from the mutating webhook. This is not strictly correct.
		validationWhConfig := fixWhConfig(tagWhConfig)

		vwhYAML, err := generateValidatingWebhook(validationWhConfig, opts.ManifestsPath)
		if err != nil {
			return "", fmt.Errorf("failed to create validating webhook: %w", err)
		}
		tagWhYAML = fmt.Sprintf(`%s
---
%s`, tagWhYAML, vwhYAML)
	}

	return tagWhYAML, nil
}

func fixWhConfig(whConfig *tagWebhookConfig) *tagWebhookConfig {
	if whConfig.URL != "" {
		webhookURL, err := url.Parse(whConfig.URL)
		if err == nil {
			webhookURL.Path = "/validate"
			whConfig.URL = webhookURL.String()
		}
	}
	return whConfig
}

// Create applies the given tag manifests.
func Create(client kube.ExtendedClient, manifests string) error {
	if err := applyYAML(client, manifests, "istio-system"); err != nil {
		return fmt.Errorf("failed to apply tag manifests to cluster: %v", err)
	}
	return nil
}

// generateValidatingWebhook renders a validating webhook configuration from the given tagWebhookConfig.
func generateValidatingWebhook(config *tagWebhookConfig, chartPath string) (string, error) {
	r := helm.NewHelmRenderer(chartPath, defaultChart, "Pilot", config.IstioNamespace, nil)

	if err := r.Run(); err != nil {
		return "", fmt.Errorf("failed running Helm renderer: %v", err)
	}

	values := fmt.Sprintf(`
global:
  istioNamespace: %s
revision: %q
base:
  validationURL: %s
`, config.IstioNamespace, config.Revision, config.URL)

	validatingWebhookYAML, err := r.RenderManifestFiltered(values, func(tmplName string) bool {
		return strings.Contains(tmplName, vwhTemplateName)
	})
	if err != nil {
		return "", fmt.Errorf("failed rendering istio-control manifest: %v", err)
	}

	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()
	serializer := json.NewSerializerWithOptions(
		json.DefaultMetaFactory, nil, nil, json.SerializerOptions{
			Yaml:   true,
			Pretty: true,
			Strict: true,
		})

	whObject, _, err := deserializer.Decode([]byte(validatingWebhookYAML), nil, &admit_v1.ValidatingWebhookConfiguration{})
	if err != nil {
		return "", fmt.Errorf("could not decode generated webhook: %w", err)
	}
	decodedWh := whObject.(*admit_v1.ValidatingWebhookConfiguration)
	for i := range decodedWh.Webhooks {
		decodedWh.Webhooks[i].ClientConfig.CABundle = []byte(config.CABundle)
	}

	whBuf := new(bytes.Buffer)
	if err = serializer.Encode(decodedWh, whBuf); err != nil {
		return "", err
	}

	return whBuf.String(), nil
}

// generateMutatingWebhook renders a mutating webhook configuration from the given tagWebhookConfig.
func generateMutatingWebhook(config *tagWebhookConfig, webhookName, chartPath string, autoInjectNamespaces bool) (string, error) {
	r := helm.NewHelmRenderer(chartPath, pilotDiscoveryChart, "Pilot", config.IstioNamespace, nil)

	if err := r.Run(); err != nil {
		return "", fmt.Errorf("failed running Helm renderer: %v", err)
	}

	values := fmt.Sprintf(`
revision: %q
revisionTags:
  - %s

sidecarInjectorWebhook:
  enableNamespacesByDefault: %t
  objectSelector:
    enabled: true
    autoInject: true

istiodRemote:
  injectionURL: %s
`, config.Revision, config.Tag, autoInjectNamespaces, config.URL)

	tagWebhookYaml, err := r.RenderManifestFiltered(values, func(tmplName string) bool {
		return strings.Contains(tmplName, revisionTagTemplateName)
	})
	if err != nil {
		return "", fmt.Errorf("failed rendering istio-control manifest: %v", err)
	}

	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()
	serializer := json.NewSerializerWithOptions(
		json.DefaultMetaFactory, nil, nil, json.SerializerOptions{
			Yaml:   true,
			Pretty: true,
			Strict: true,
		})

	whObject, _, err := deserializer.Decode([]byte(tagWebhookYaml), nil, &admit_v1.MutatingWebhookConfiguration{})
	if err != nil {
		return "", fmt.Errorf("could not decode generated webhook: %w", err)
	}
	decodedWh := whObject.(*admit_v1.MutatingWebhookConfiguration)
	for i := range decodedWh.Webhooks {
		decodedWh.Webhooks[i].ClientConfig.CABundle = []byte(config.CABundle)
		if decodedWh.Webhooks[i].ClientConfig.Service != nil {
			decodedWh.Webhooks[i].ClientConfig.Service.Path = &config.Path
		}
	}
	if webhookName != "" {
		decodedWh.Name = webhookName
	}

	whBuf := new(bytes.Buffer)
	if err = serializer.Encode(decodedWh, whBuf); err != nil {
		return "", err
	}

	return whBuf.String(), nil
}

// tagWebhookConfigFromCanonicalWebhook parses configuration needed to create tag webhook from existing revision webhook.
func tagWebhookConfigFromCanonicalWebhook(wh admit_v1.MutatingWebhookConfiguration, tagName, istioNS string) (*tagWebhookConfig, error) {
	rev, err := GetWebhookRevision(wh)
	if err != nil {
		return nil, err
	}
	// if the revision is "default", render templates with an empty revision
	if rev == DefaultRevisionName {
		rev = ""
	}

	var injectionURL, caBundle, path string
	found := false
	for _, w := range wh.Webhooks {
		if strings.HasSuffix(w.Name, istioInjectionWebhookSuffix) {
			found = true
			caBundle = string(w.ClientConfig.CABundle)
			if w.ClientConfig.URL != nil {
				injectionURL = *w.ClientConfig.URL
			}
			if w.ClientConfig.Service != nil {
				if w.ClientConfig.Service.Path != nil {
					path = *w.ClientConfig.Service.Path
				}
			}
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("could not find sidecar-injector webhook in canonical webhook %q", wh.Name)
	}

	return &tagWebhookConfig{
		Tag:            tagName,
		Revision:       rev,
		URL:            injectionURL,
		CABundle:       caBundle,
		IstioNamespace: istioNS,
		Path:           path,
	}, nil
}

// applyYAML taken from remote_secret.go
func applyYAML(client kube.ExtendedClient, yamlContent, ns string) error {
	yamlFile, err := writeToTempFile(yamlContent)
	if err != nil {
		return fmt.Errorf("failed creating manifest file: %w", err)
	}

	// Apply the YAML to the cluster.
	if err := client.ApplyYAMLFiles(ns, yamlFile); err != nil {
		return fmt.Errorf("failed applying manifest %s: %v", yamlFile, err)
	}
	return nil
}

// writeToTempFile taken from remote_secret.go
func writeToTempFile(content string) (string, error) {
	outFile, err := os.CreateTemp("", "revision-tag-manifest-*")
	if err != nil {
		return "", fmt.Errorf("failed creating temp file for manifest: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	if _, err := outFile.Write([]byte(content)); err != nil {
		return "", fmt.Errorf("failed writing manifest file: %w", err)
	}
	return outFile.Name(), nil
}
