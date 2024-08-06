package webhook

import (
	"context"
	"fmt"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/sets"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
	"strings"

	revtag "istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/webhook"
	"istio.io/istio/pkg/config/analysis/local"
)

func CheckWebhooks(manifests []manifest.ManifestSet, iop values.Map, clt kube.Client) error {
	pilotManifests := manifest.ExtractComponent(manifests, component.PilotComponentName)
	if len(pilotManifests) == 0 {
		return nil
	}

	// Add webhook manifests to be applied
	var localWebhookYAMLReaders []local.ReaderSource
	exists := revtag.PreviousInstallExists(context.Background(), clt.Kube())
	needed := detectIfTagWebhookIsNeeded(iop, exists)
	webhookNames := sets.New[string]()
	for i, wh := range pilotManifests {
		if wh.GetKind() != gvk.MutatingWebhookConfiguration.Kind {
			continue
		}
		webhookNames.Insert(wh.GetName())
		// Here if we need to create a default tag, we need to skip the webhooks that are going to be deactivated.
		if !needed {
			whReaderSource := local.ReaderSource{
				Name:   fmt.Sprintf("installed-webhook-%d", i),
				Reader: strings.NewReader(wh.Content),
			}
			localWebhookYAMLReaders = append(localWebhookYAMLReaders, whReaderSource)
		}
	}

	sa := local.NewSourceAnalyzer(analysis.Combine("webhook", &webhook.Analyzer{
		SkipServiceCheck:             true,
		SkipDefaultRevisionedWebhook: needed,
	}), "", "", nil)

	// Add in-cluster webhooks
	inCluster, err := clt.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i, obj := range inCluster.Items {
		objYAML, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		whReaderSource := local.ReaderSource{
			Name:   fmt.Sprintf("in-cluster-webhook-%d", i),
			Reader: strings.NewReader(string(objYAML)),
		}
		err = sa.AddReaderKubeSource([]local.ReaderSource{whReaderSource})
		if err != nil {
			return err
		}
	}

	err = sa.AddReaderKubeSource(localWebhookYAMLReaders)
	if err != nil {
		return err
	}

	// Analyze webhooks
	res, err := sa.Analyze(make(chan struct{}))
	if err != nil {
		return err
	}
	relevantMessages := filterOutBasedOnResources(res.Messages, webhookNames)
	if len(relevantMessages) > 0 {
		o, err := formatting.Print(relevantMessages, formatting.LogFormat, false)
		if err != nil {
			return err
		}
		return fmt.Errorf("creating default tag would conflict:\n%v", o)
	}
	return nil
}

func filterOutBasedOnResources(messages diag.Messages, names sets.Set[string]) diag.Messages {
	outputMessages := diag.Messages{}
	for _, m := range messages {
		if names.Contains(m.Resource.Metadata.FullName.Name.String()) {
			outputMessages = append(outputMessages, m)
		}
	}
	return outputMessages
}

func detectIfTagWebhookIsNeeded(iop values.Map, exists bool) bool {
	rev := values.TryGetPathAs[string](iop, "spec.values.revision")
	operatorManageWebhooks := values.TryGetPathAs[bool](iop, "spec.values.global.operatorManageWebhooks")
	isDefaultInstallation := rev == ""
	return !operatorManageWebhooks && (!exists || isDefaultInstallation)
}
