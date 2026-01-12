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

package webhook

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	revtag "istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/webhook"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

func WebhooksToDeploy(iop values.Map, clt kube.Client, baseLabels map[string]string, dryRun bool) ([]manifest.Manifest, error) {
	exists := revtag.PreviousInstallExists(context.Background(), clt.Kube())
	needed := detectIfTagWebhookIsNeeded(iop, exists)
	if !needed {
		return nil, nil
	}
	rev := ptr.NonEmptyOrDefault(iop.GetPathString("spec.values.revision"), "default")
	autoInject := iop.GetPathBool("spec.values.sidecarInjectorWebhook.enableNamespacesByDefault")

	customLabels := map[string]string{}
	for label, val := range baseLabels {
		customLabels[label] = val
	}
	customLabels[manifest.OwningResourceNotPruned] = "true"
	ns := ptr.NonEmptyOrDefault(iop.GetPathString("metadata.namespace"), "istio-system")
	o := &revtag.GenerateOptions{
		Tag:                  revtag.DefaultRevisionName,
		Revision:             rev,
		Overwrite:            true,
		AutoInjectNamespaces: autoInject,
		CustomLabels:         customLabels,
		Generate:             dryRun,
		IstioNamespace:       ns,
	}
	// If tag cannot be created could be remote cluster install, don't fail out.
	tagManifests, err := revtag.Generate(context.Background(), clt, o)
	if err != nil {
		return nil, nil
	}
	return manifest.ParseMultiple(tagManifests)
}

func CheckWebhooks(manifests []manifest.ManifestSet, iop values.Map, clt kube.Client, logger clog.Logger) error {
	pilotManifests := manifest.ExtractComponent(manifests, component.PilotComponentName)
	if len(pilotManifests) == 0 {
		return nil
	}

	// Add webhook manifests to be applied
	var localWebhookYAMLReaders []local.ReaderSource
	exists := revtag.PreviousInstallExists(context.Background(), clt.Kube())
	rev := iop.GetPathString("spec.values.revision")
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
		obj.TypeMeta.Kind = "MutatingWebhookConfiguration"
		obj.TypeMeta.APIVersion = "admissionregistration.k8s.io/v1"
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

	// Check if we would be changing the default webhook
	if needed {
		mwhs, err := clt.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=sidecar-injector,istio.io/rev=default,istio.io/tag=default",
		})
		if err != nil {
			return err
		}
		// If there is no default webhook but a revisioned default webhook exists,
		// and we are installing a new IOP with default semantics, the default webhook shifts.
		if exists && len(mwhs.Items) == 0 && rev == "" {
			logger.Print("This installation will make default injection and validation pointing to the default revision, and " +
				"originally it was pointing to the revisioned one.\n")
		}
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
	rev := iop.GetPathString("spec.values.revision")
	operatorManageWebhooks := iop.GetPathBool("spec.values.global.operatorManageWebhooks")
	isDefaultInstallation := rev == ""
	return !operatorManageWebhooks && (!exists || isDefaultInstallation)
}
