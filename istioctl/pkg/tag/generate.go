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

	// Needed to embed webhook templates.
	_ "embed"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"strings"
	"text/template"

	admit_v1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/pkg/kube"
)

const (
	IstioTagLabel       = "istio.io/tag"
	DefaultRevisionName = "default"

	pilotDiscoveryChart          = "istio-control/istio-discovery"
	revisionTagTemplateName      = "revision-tags.yaml"
	istioInjectionWebhookSuffix  = "sidecar-injector.istio.io"
	istioValidationWebhookSuffix = "validation.istio.io"
)

// tagWebhookConfig holds config needed to render a tag webhook.
type tagWebhookConfig struct {
	Tag            string
	Revision       string
	URL            string
	CABundle       string // note: this is raw PEM bytes, should base64 encode before inserting
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
	// applying is not done here but we are looser with checks when doing generate.
	Generate bool
	// Overwrite removes analysis checks around existing webhooks.
	Overwrite bool
}

//go:embed templates/validatingwebhook.yaml
var validatingWebhookTemplate string

// Generate generates the manifests for a revision tag pointed the given revision.
func Generate(ctx context.Context, client kube.ExtendedClient, opts *GenerateOptions) (string, error) {
	// abort if there exists a revision with the target tag name
	revWebhookCollisions, err := GetWebhooksWithRevision(ctx, client, opts.Tag)
	if err != nil {
		return "", err
	}
	if !opts.Generate && !opts.Overwrite &&
		len(revWebhookCollisions) > 0 && opts.Tag != DefaultRevisionName {
		return "", fmt.Errorf("cannot create revision tag %q: found existing control plane revision with same name", opts.Tag)
	}

	// find canonical revision webhook to base our tag webhook off of
	revWebhooks, err := GetWebhooksWithRevision(ctx, client, opts.Revision)
	if err != nil {
		return "", err
	}
	if len(revWebhooks) == 0 {
		return "", fmt.Errorf("cannot modify tag: cannot find MutatingWebhookConfiguration with revision %q", opts.Revision)
	}
	if len(revWebhooks) > 1 {
		return "", fmt.Errorf("cannot modify tag: found multiple canonical webhooks with revision %q", opts.Revision)
	}

	whs, err := GetWebhooksWithTag(ctx, client, opts.Tag)
	if err != nil {
		return "", err
	}
	if len(whs) > 0 && !opts.Overwrite {
		return "", fmt.Errorf("revision tag %q already exists, and --overwrite is false", opts.Tag)
	}

	tagWhConfig, err := tagWebhookConfigFromCanonicalWebhook(revWebhooks[0], opts.Tag)
	if err != nil {
		return "", fmt.Errorf("failed to create tag webhook config: %w", err)
	}
	tagWhYAML, err := generateMutatingWebhook(tagWhConfig, opts.WebhookName, opts.ManifestsPath)
	if err != nil {
		return "", fmt.Errorf("failed to create tag webhook: %w", err)
	}

	if opts.Tag == DefaultRevisionName {
		// deactivate other istio-injection=enabled injectors if using default revisions.
		err := DeactivateIstioInjectionWebhook(ctx, client)
		if err != nil {
			return "", fmt.Errorf("failed deactivating existing default revision: %w", err)
		}
		// TODO(Monkeyanator) should extract the validationURL from revision's validating webhook here. However,
		// to ease complexity when pointing default to revision without per-revision validating webhook,
		// instead grab the endpoint information from the mutating webhook. This is not strictly correct.
		vwhYAML, err := generateValidatingWebhook(tagWhConfig)
		if err != nil {
			return "", fmt.Errorf("failed to create validating webhook: %w", err)
		}
		tagWhYAML = fmt.Sprintf(`%s
---
%s`, tagWhYAML, vwhYAML)
	}

	return tagWhYAML, nil
}

// Create applies the given tag manifests.
func Create(client kube.ExtendedClient, manifests string) error {
	if err := applyYAML(client, manifests, "istio-system"); err != nil {
		return fmt.Errorf("failed to apply tag manifests to cluster: %v", err)
	}
	return nil
}

// generateValidatingWebhook renders a validating webhook configuration from the given tagWebhookConfig.
func generateValidatingWebhook(config *tagWebhookConfig) (string, error) {
	modified := *config
	modified.CABundle = base64.StdEncoding.EncodeToString([]byte(config.CABundle))
	tmpl, err := template.New("vwh").Parse(validatingWebhookTemplate)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, modified)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateMutatingWebhook renders a mutating webhook configuration from the given tagWebhookConfig.
func generateMutatingWebhook(config *tagWebhookConfig, webhookName, chartPath string) (string, error) {
	r := helm.NewHelmRenderer(chartPath, pilotDiscoveryChart, "Pilot", config.IstioNamespace)

	if err := r.Run(); err != nil {
		return "", fmt.Errorf("failed running Helm renderer: %v", err)
	}

	values := fmt.Sprintf(`
revision: %q
revisionTags:
  - %s

sidecarInjectorWebhook:
  objectSelector:
    enabled: true
    autoInject: true

istiodRemote:
  injectionURL: %s
`, config.Revision, config.Tag, config.URL)

	tagWebhookYaml, err := r.RenderManifestFiltered(values, func(tmplName string) bool {
		return strings.Contains(tmplName, revisionTagTemplateName)
	})
	if err != nil {
		return "", fmt.Errorf("failed rendering istio-control manifest: %v", err)
	}

	// Need to deserialize webhook to change CA bundle
	// and serialize it back into YAML
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
func tagWebhookConfigFromCanonicalWebhook(wh admit_v1.MutatingWebhookConfiguration, tagName string) (*tagWebhookConfig, error) {
	rev, err := GetWebhookRevision(wh)
	if err != nil {
		return nil, err
	}
	// if the revision is "default", render templates with an empty revision
	if rev == DefaultRevisionName {
		rev = ""
	}

	var injectionURL string
	var caBundle string
	found := false
	for _, w := range wh.Webhooks {
		if strings.HasSuffix(w.Name, istioInjectionWebhookSuffix) {
			found = true
			caBundle = string(w.ClientConfig.CABundle)
			if w.ClientConfig.URL != nil {
				injectionURL = *w.ClientConfig.URL
			} else {
				injectionURL = ""
			}
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("could not find sidecar-injector webhook in canonical webhook")
	}

	return &tagWebhookConfig{
		Tag:            tagName,
		Revision:       rev,
		URL:            injectionURL,
		CABundle:       caBundle,
		IstioNamespace: "istio-system",
	}, nil
}

// tagWebhookConfigFromValidatingWebhook parses configuration needed for validating webhook configuration.
func tagWebhookConfigFromValidatingWebhook(wh admit_v1.ValidatingWebhookConfiguration, tagName string) (*tagWebhookConfig, error) {
	rev, ok := wh.ObjectMeta.Labels[label.IoIstioRev.Name]
	if !ok {
		return nil, fmt.Errorf("could not extract revision from webhook")
	}
	// if the revision is "default", render templates with an empty revision
	if rev == DefaultRevisionName {
		rev = ""
	}

	var injectionURL string
	var caBundle string
	found := false
	for _, w := range wh.Webhooks {
		if strings.HasSuffix(w.Name, istioValidationWebhookSuffix) {
			found = true
			caBundle = string(w.ClientConfig.CABundle)
			if w.ClientConfig.URL != nil {
				injectionURL = *w.ClientConfig.URL
			} else {
				injectionURL = ""
			}
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("could not find sidecar-injector webhook in canonical webhook")
	}

	return &tagWebhookConfig{
		Tag:            tagName,
		Revision:       rev,
		URL:            injectionURL,
		CABundle:       caBundle,
		IstioNamespace: "istio-system",
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
	outFile, err := ioutil.TempFile("", "revision-tag-manifest-*")
	if err != nil {
		return "", fmt.Errorf("failed creating temp file for manifest: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	if _, err := outFile.Write([]byte(content)); err != nil {
		return "", fmt.Errorf("failed writing manifest file: %w", err)
	}
	return outFile.Name(), nil
}
