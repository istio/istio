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
	"strconv"
	"strings"

	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/render"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
)

const (
	DefaultRevisionName = "default"

	istioInjectionWebhookSuffix = "sidecar-injector.istio.io"

	vwhBaseTemplateName = "istiod-default-validator"

	operatorNamespace = "operator.istio.io"
)

// tagWebhookConfig holds config needed to render a tag webhook.
type tagWebhookConfig struct {
	WebHookService string
	Tag            string
	Revision       string
	URL            string
	Path           string
	CABundle       string
	IstioNamespace string
	Labels         map[string]string
	Annotations    map[string]string
	// FailurePolicy records the failure policy to use for the webhook.
	FailurePolicy map[string]*admitv1.FailurePolicyType
	// ReinvocationPolicy records the reinvocation policy to use for the webhook.
	ReinvocationPolicy string
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
	// CustomLabels are labels to add to the generated webhook.
	CustomLabels map[string]string
	// UserManaged indicates whether the revision tag is user managed.
	// If true, the revision tag will not be affected by the installer.
	UserManaged bool
	// IstioNamespace indicates the namespace of the istio installation.
	IstioNamespace string
}

// Generate generates the manifests for a revision tag pointed the given revision.
func Generate(ctx context.Context, client kube.Client, opts *GenerateOptions) (string, error) {
	// abort if there exists a revision with the target tag name
	isRunningAmbient, err := checkIfRevisionIsRunningAmbient(ctx, client.Kube(), opts.Revision, opts.IstioNamespace)
	if err != nil {
		return "", err
	}
	err = checkTagNameCollidesWithRevisionName(ctx, client.Kube(), isRunningAmbient, opts)
	if err != nil {
		return "", err
	}

	// TODO: Use canonService from here to create a new service
	_, canonWebhook, err := checkControlPlaneExistenceOrDuplicate(ctx, client.Kube(), isRunningAmbient, opts)
	if err != nil {
		return "", err
	}
	err = checkTagDuplicate(ctx, client.Kube(), isRunningAmbient, opts)
	if err != nil {
		return "", err
	}

	tagWhConfig, err := tagWebhookConfigFromCanonicalWebhook(*canonWebhook, opts.Tag, opts.IstioNamespace)
	if err != nil {
		return "", fmt.Errorf("failed to create tag webhook config: %w", err)
	}
	tagWhYAML, err := generateMutatingWebhook(tagWhConfig, opts)
	var vwhYAML string
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
		}

		// TODO(Monkeyanator) should extract the validationURL from revision's validating webhook here. However,
		// to ease complexity when pointing default to revision without per-revision validating webhook,
		// instead grab the endpoint information from the mutating webhook. This is not strictly correct.
		validationWhConfig, err := fixWhConfig(client, tagWhConfig)
		if err != nil {
			return "", fmt.Errorf("failed to create validating webhook config: %w", err)
		}

		vwhYAML, err = generateValidatingWebhook(validationWhConfig, opts)
		if err != nil {
			return "", fmt.Errorf("failed to create validating webhook: %w", err)
		}
	}
	var tagServiceYAML string
	if isRunningAmbient {
		tagServiceYAML, err = generateTagService(opts)
		if err != nil {
			return "", err
		}
	}

	resources := []string{
		tagWhYAML,
		vwhYAML,
		tagServiceYAML,
	}
	resourcesStrings := []string{}
	for _, resource := range resources {
		if resource == "" {
			continue
		}
		resourcesStrings = append(resourcesStrings, resource)
	}

	return strings.Join(resourcesStrings, "\n---\n"), nil
}

// checkTagNameCollidesWithRevisionName returns an error if user attempts to
// override a revision using a tag name
func checkTagNameCollidesWithRevisionName(
	ctx context.Context,
	client kubernetes.Interface,
	isAmbientMode bool,
	opts *GenerateOptions,
) error {
	if opts.Generate || opts.Overwrite || opts.Tag == DefaultRevisionName {
		return nil
	}
	existingControlPlaneErr := fmt.Errorf("cannot create revision tag %q: found existing control plane revision with same name", opts.Tag)

	if isAmbientMode {
		revServiceCollisions, err := GetServicesWithRevision(ctx, client, opts.IstioNamespace, opts.Tag)
		if err != nil {
			return err
		}
		if len(revServiceCollisions) > 0 {
			return existingControlPlaneErr
		}
	}
	// abort if there exists a revision with the target tag name
	revWebhookCollisions, err := GetWebhooksWithRevision(ctx, client, opts.Tag)
	if err != nil {
		return err
	}
	if len(revWebhookCollisions) > 0 {
		return existingControlPlaneErr
	}
	return nil
}

func checkControlPlaneExistenceOrDuplicate(
	ctx context.Context,
	client kubernetes.Interface,
	isAmbientMode bool,
	opts *GenerateOptions,
) (*corev1.Service, *admitv1.MutatingWebhookConfiguration, error) {
	var service *corev1.Service = nil
	if isAmbientMode {
		revServices, err := GetServicesWithRevision(ctx, client, opts.IstioNamespace, opts.Revision)
		if err != nil {
			return nil, nil, err
		}

		if len(revServices) == 0 {
			return nil, nil, fmt.Errorf("cannot modify tag: cannot find Service with revision %q in namespace %q", opts.Revision, opts.IstioNamespace)
		}
		if len(revServices) > 1 {
			return nil, nil, fmt.Errorf("cannot modify tag: found multiple canonical services with revision %q in namespace %q", opts.Revision, opts.IstioNamespace)
		}
		service = &revServices[0]
	}
	revWebhooks, err := GetWebhooksWithRevision(ctx, client, opts.Revision)
	if err != nil {
		return nil, nil, err
	}
	if len(revWebhooks) == 0 {
		return nil, nil, fmt.Errorf("cannot modify tag: cannot find MutatingWebhookConfiguration with revision %q", opts.Revision)
	}
	if len(revWebhooks) > 1 {
		return nil, nil, fmt.Errorf("cannot modify tag: found multiple canonical webhooks with revision %q", opts.Revision)
	}
	return service, &revWebhooks[0], nil
}

func checkTagDuplicate(
	ctx context.Context,
	client kubernetes.Interface,
	isAmbientMode bool,
	opts *GenerateOptions,
) error {
	if opts.Overwrite {
		return nil
	}

	if isAmbientMode {
		tagServices, err := GetServicesWithTag(ctx, client, opts.IstioNamespace, opts.Tag)
		if err != nil {
			return err
		}
		if len(tagServices) > 0 {
			return fmt.Errorf("revision tag %q already exists, and --overwrite is false", opts.Tag)
		}
	}
	whs, err := GetWebhooksWithTag(ctx, client, opts.Tag)
	if err != nil {
		return err
	}
	if len(whs) > 0 {
		return fmt.Errorf("revision tag %q already exists, and --overwrite is false", opts.Tag)
	}
	return nil
}

func fixWhConfig(client kube.Client, whConfig *tagWebhookConfig) (*tagWebhookConfig, error) {
	if whConfig.URL != "" {
		webhookURL, err := url.Parse(whConfig.URL)
		if err == nil {
			webhookURL.Path = "/validate"
			whConfig.URL = webhookURL.String()
		}
	}

	// ValidatingWebhookConfiguration failurePolicy is managed by Istiod, so if currently we already have a webhook in cluster
	// that is set to `Fail` by Istiod, we avoid of setting it back to the default `Ignore`.
	vwh, err := client.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().
		Get(context.Background(), vwhBaseTemplateName, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	if vwh == nil {
		return whConfig, nil
	}
	if whConfig.FailurePolicy == nil {
		whConfig.FailurePolicy = map[string]*admitv1.FailurePolicyType{}
	}
	for _, wh := range vwh.Webhooks {
		if wh.FailurePolicy != nil && *wh.FailurePolicy == admitv1.Fail {
			whConfig.FailurePolicy[wh.Name] = nil
		} else {
			whConfig.FailurePolicy[wh.Name] = wh.FailurePolicy
		}
	}
	return whConfig, nil
}

// Create applies the given tag manifests.
func Create(client kube.CLIClient, manifests, ns string) error {
	if err := client.ApplyYAMLContents(ns, manifests); err != nil {
		return fmt.Errorf("failed to apply tag manifests to cluster: %v", err)
	}
	return nil
}

// generateValidatingWebhook renders a validating webhook configuration from the given tagWebhookConfig.
func generateValidatingWebhook(config *tagWebhookConfig, opts *GenerateOptions) (string, error) {
	vals := values.Map{
		"spec": values.Map{
			"installPackagePath": opts.ManifestsPath,
			"values": values.Map{
				"revision": config.Revision,
				"base":     values.Map{"validationURL": config.URL},
				"global":   values.Map{"istioNamespace": config.IstioNamespace},
			},
		},
	}
	mfs, _, err := helm.Render("istio", config.IstioNamespace, "default", vals, nil)
	if err != nil {
		return "", nil
	}
	var validatingWebhookYAML string
	for _, m := range mfs {
		if m.GetKind() == "ValidatingWebhookConfiguration" {
			validatingWebhookYAML = m.Content
			break
		}
	}
	// TODO: Evaluate if we need to return a custom error to handle outside
	if validatingWebhookYAML == "" {
		return "", fmt.Errorf("could not find ValidatingWebhookConfiguration in manifests")
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

	whObject, _, err := deserializer.Decode([]byte(validatingWebhookYAML), nil, &admitv1.ValidatingWebhookConfiguration{})
	if err != nil {
		return "", fmt.Errorf("could not decode generated webhook: %w", err)
	}
	decodedWh := whObject.(*admitv1.ValidatingWebhookConfiguration)
	for i := range decodedWh.Webhooks {
		decodedWh.Webhooks[i].ClientConfig.CABundle = []byte(config.CABundle)
	}
	decodedWh.Labels = generateLabels(decodedWh.Labels, config.Labels, opts.CustomLabels, opts.UserManaged)
	decodedWh.Annotations = maps.MergeCopy(decodedWh.Annotations, config.Annotations)
	for i := range decodedWh.Webhooks {
		if failurePolicy, ok := config.FailurePolicy[decodedWh.Webhooks[i].Name]; ok {
			decodedWh.Webhooks[i].FailurePolicy = failurePolicy
		}
	}
	whBuf := new(bytes.Buffer)
	if err = serializer.Encode(decodedWh, whBuf); err != nil {
		return "", err
	}

	return whBuf.String(), nil
}

func generateLabels(whLabels, curLabels, customLabels map[string]string, userManaged bool) map[string]string {
	whLabels = maps.MergeCopy(whLabels, curLabels)
	whLabels = maps.MergeCopy(whLabels, customLabels)
	if userManaged {
		for label := range whLabels {
			if strings.Contains(label, operatorNamespace) {
				delete(whLabels, label)
			}
		}
	}
	return whLabels
}

func generateTagService(opts *GenerateOptions) (string, error) {
	flags := []string{
		"installPackagePath=" + opts.ManifestsPath,
		"profile=empty",
		"components.pilot.enabled=true",
		"revision=" + opts.Revision,
		"values.revisionTags.[0]=" + opts.Tag,
		// TODO: How to handle auto inject namespaces for ambient?
		"values.sidecarInjectorWebhook.enableNamespacesByDefault=" + strconv.FormatBool(opts.AutoInjectNamespaces),
		"values.global.istioNamespace=" + opts.IstioNamespace,
	}

	mfs, _, err := render.GenerateManifest(nil, flags, false, nil, nil)
	if err != nil {
		return "", err
	}
	var tagServiceYaml string
	for _, mf := range mfs {
		for _, m := range mf.Manifests {
			tag := m.GetLabels()[label.IoIstioTag.Name]
			if m.GetKind() == "Service" && tag == opts.Tag {
				tagServiceYaml = m.Content
				break
			}
		}
	}
	if tagServiceYaml == "" {
		return "", fmt.Errorf("could not find Service tag in manifests")
	}
	return tagServiceYaml, nil
}

// generateMutatingWebhook renders a mutating webhook configuration from the given tagWebhookConfig.
func generateMutatingWebhook(config *tagWebhookConfig, opts *GenerateOptions) (string, error) {
	flags := []string{
		"installPackagePath=" + opts.ManifestsPath,
		"profile=empty",
		"components.pilot.enabled=true",
		"revision=" + config.Revision,
		"values.revisionTags.[0]=" + config.Tag,
		"values.sidecarInjectorWebhook.enableNamespacesByDefault=" + strconv.FormatBool(opts.AutoInjectNamespaces),
		"values.istiodRemote.injectionURL=" + config.URL,
		"values.global.istioNamespace=" + config.IstioNamespace,
	}
	if len(config.ReinvocationPolicy) > 0 {
		flags = append(flags, "values.sidecarInjectorWebhook.reinvocationPolicy="+config.ReinvocationPolicy)
	}
	mfs, _, err := render.GenerateManifest(nil, flags, false, nil, nil)
	if err != nil {
		return "", err
	}
	var tagWebhookYaml string
	for _, mf := range mfs {
		for _, m := range mf.Manifests {
			if m.GetKind() == "MutatingWebhookConfiguration" && strings.HasPrefix(m.GetName(), "istio-revision-tag-") {
				tagWebhookYaml = m.Content
				break
			}
		}
	}
	// TODO: Might need to return a custom made error to handle not found
	if tagWebhookYaml == "" {
		return "", fmt.Errorf("could not find MutatingWebhookConfiguration in manifests")
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

	whObject, _, err := deserializer.Decode([]byte(tagWebhookYaml), nil, &admitv1.MutatingWebhookConfiguration{})
	if err != nil {
		return "", fmt.Errorf("could not decode generated webhook: %w", err)
	}
	decodedWh := whObject.(*admitv1.MutatingWebhookConfiguration)
	for i := range decodedWh.Webhooks {
		decodedWh.Webhooks[i].ClientConfig.CABundle = []byte(config.CABundle)
		if decodedWh.Webhooks[i].ClientConfig.Service != nil {
			decodedWh.Webhooks[i].ClientConfig.Service.Path = &config.Path
			// if profile=remote, need specify service istiod-remote to compatibility
			decodedWh.Webhooks[i].ClientConfig.Service.Name = config.WebHookService
		}
	}
	if opts.WebhookName != "" {
		decodedWh.Name = opts.WebhookName
	}
	decodedWh.Labels = generateLabels(decodedWh.Labels, config.Labels, opts.CustomLabels, opts.UserManaged)
	decodedWh.Annotations = maps.MergeCopy(decodedWh.Annotations, config.Annotations)
	whBuf := new(bytes.Buffer)
	if err = serializer.Encode(decodedWh, whBuf); err != nil {
		return "", err
	}

	return whBuf.String(), nil
}

// tagWebhookConfigFromCanonicalWebhook parses configuration needed to create tag webhook from existing revision webhook.
func tagWebhookConfigFromCanonicalWebhook(wh admitv1.MutatingWebhookConfiguration, tagName, istioNS string) (*tagWebhookConfig, error) {
	rev, err := GetWebhookRevision(wh)
	if err != nil {
		return nil, err
	}
	// if the revision is "default", render templates with an empty revision
	if rev == DefaultRevisionName {
		rev = ""
	}

	var injectionURL, caBundle, path, reinvocationPolicy, service string
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
				service = w.ClientConfig.Service.Name
			}
			if w.ReinvocationPolicy != nil {
				reinvocationPolicy = string(*w.ReinvocationPolicy)
			}
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("could not find sidecar-injector webhook in canonical webhook %q", wh.Name)
	}

	// Here we filter out the "app" label, to generate a general label set for the incoming generated
	// MutatingWebhookConfiguration and ValidatingWebhookConfiguration. The app of the webhooks are not general
	// since they are functioned differently with different name.
	// The filtered common labels are then added to the incoming generated
	// webhooks, which aids in managing these webhooks via the istioctl/operator.
	filteredLabels := make(map[string]string)
	for k, v := range wh.Labels {
		if k != "app" {
			filteredLabels[k] = v
		}
	}

	return &tagWebhookConfig{
		WebHookService:     service,
		Tag:                tagName,
		Revision:           rev,
		URL:                injectionURL,
		CABundle:           caBundle,
		IstioNamespace:     istioNS,
		Path:               path,
		Labels:             filteredLabels,
		Annotations:        wh.Annotations,
		FailurePolicy:      map[string]*admitv1.FailurePolicyType{},
		ReinvocationPolicy: reinvocationPolicy,
	}, nil
}

func checkIfRevisionIsRunningAmbient(ctx context.Context, client kubernetes.Interface, rev string, istioNS string) (bool, error) {
	// Construct the deployment name based on the revision
	deploymentName := "istiod"
	if rev != "" {
		deploymentName += "-" + rev
	}

	// Get the deployment in the specified namespace
	deployment, err := client.AppsV1().Deployments(istioNS).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment not found, return false
			return false, nil
		}
		// Return any other error
		return false, err
	}

	// Check if the deployment has the "istio.io/controlplane-mode" label set to "ambient"
	if mode, exists := deployment.Labels["istio.io/controlplane-mode"]; exists && mode == "ambient" {
		return true, nil
	}

	// The deployment is not running in ambient mode
	return false, nil
}
