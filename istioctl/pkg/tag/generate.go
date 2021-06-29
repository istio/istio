package tag

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"

	"istio.io/istio/operator/pkg/helm"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

const (
	IstioTagLabel       = "istio.io/tag"
	DefaultRevisionName = "default"

	pilotDiscoveryChart         = "istio-control/istio-discovery"
	revisionTagTemplateName     = "revision-tags.yaml"
	istioInjectionWebhookSuffix = "sidecar-injector.istio.io"
)

// TagWebhookConfig holds config needed to render a tag webhook.
type TagWebhookConfig struct {
	Tag            string
	Revision       string
	URL            string
	CABundle       string // note: this is treated as raw PEM value, should base64 encode before inserting
	IstioNamespace string
}

//go:embed templates/validatingwebhook.yaml
var validatingWebhookTemplate string

// GenerateValidatingWebhook renders a validating webhook configuration from the given TagWebhookConfig.
func GenerateValidatingWebhook(config *TagWebhookConfig) (string, error) {
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

// GenerateMutatingWebhook renders a mutating webhook configuration from the given TagWebhookConfig.
func GenerateMutatingWebhook(config *TagWebhookConfig, webhookName, chartPath string) (string, error) {
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

// TagWebhookConfigFromCanonicalWebhook parses configuration needed to create tag webhook from existing revision webhook.
func TagWebhookConfigFromCanonicalWebhook(wh admit_v1.MutatingWebhookConfiguration, tagName string) (*TagWebhookConfig, error) {
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

	return &TagWebhookConfig{
		Tag:            tagName,
		Revision:       rev,
		URL:            injectionURL,
		CABundle:       caBundle,
		IstioNamespace: "istio-system",
	}, nil
}
