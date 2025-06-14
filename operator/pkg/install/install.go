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

package install

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/uninstall"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/operator/pkg/webhook"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/istiomultierror"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/version"
)

type Installer struct {
	Force          bool
	DryRun         bool
	SkipWait       bool
	Kube           kube.CLIClient
	Values         values.Map
	WaitTimeout    time.Duration
	Logger         clog.Logger
	ProgressLogger *progress.Log
}

// InstallManifests applies a set of rendered manifests to the cluster.
func (i Installer) InstallManifests(manifests []manifest.ManifestSet) error {
	// The namespace is not a part of the manifest generation, but needed to actually deploy to the cluster.
	// Install if needed.
	err := i.installSystemNamespace()
	if err != nil {
		return err
	}

	// Precheck to ensure we do not deploy conflicting webhooks.
	if err := webhook.CheckWebhooks(manifests, i.Values, i.Kube, i.Logger); err != nil {
		if i.Force {
			i.Logger.LogAndErrorf("invalid webhook configs; continuing because of --force: %v", err)
		} else {
			return err
		}
	}

	// Finally, we can actually install all the manifests
	if err := i.install(manifests); err != nil {
		return err
	}

	// We may need to manually deploy some webhooks out-of-band from the install, making this th
	ownerLabels := getOwnerLabels(i.Values, "")
	webhooks, err := webhook.WebhooksToDeploy(i.Values, i.Kube, ownerLabels, i.DryRun)
	if err != nil {
		return fmt.Errorf("failed generating webhooks: %v", err)
	}
	for _, wh := range webhooks {
		if err := i.serverSideApply(wh); err != nil {
			return fmt.Errorf("failed deploying webhooks: %v", err)
		}
	}

	return nil
}

// installSystemNamespace creates the system namespace before install
func (i Installer) installSystemNamespace() error {
	ns := i.Values.GetPathStringOr("metadata.namespace", "istio-system")
	network := i.Values.GetPathString("spec.values.global.network")
	if err := util.CreateNamespace(i.Kube.Kube(), ns, network, i.DryRun); err != nil {
		return err
	}
	return nil
}

// install takes rendered manifests and actually applies them to the cluster. This takes into account ordering based on component.
func (i Installer) install(manifests []manifest.ManifestSet) error {
	var mu sync.Mutex
	errors := istiomultierror.New()
	// wg waits for all manifest processing goroutines to finish
	var wg sync.WaitGroup

	disabledComponents := sets.New(slices.Map(component.AllComponents, func(e component.Component) component.Name {
		return e.UserFacingName
	})...)
	dependencyWaitCh := dependenciesChannels()
	for _, mf := range manifests {
		c := mf.Component
		ms := mf.Manifests
		disabledComponents.Delete(c)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Wait for all of our dependencies to finish...
			if s := dependencyWaitCh[c]; s != nil {
				<-s
			}

			// Apply all the manifests
			if len(ms) != 0 {
				if err := i.applyManifestSet(mf); err != nil {
					mu.Lock()
					errors = multierror.Append(errors, err)
					mu.Unlock()
				}
			}

			// Signal all the components that depend on us.
			for _, ch := range componentDependencies[c] {
				dependencyWaitCh[ch] <- struct{}{}
			}
		}()
	}
	// For any components we did not install, mark them as "done"
	for c := range disabledComponents {
		// Signal all the components that depend on us.
		for _, ch := range componentDependencies[c] {
			dependencyWaitCh[ch] <- struct{}{}
		}
	}
	wg.Wait()
	if err := errors.ErrorOrNil(); err != nil {
		return err
	}

	if err := i.prune(manifests); err != nil {
		return fmt.Errorf("pruning: %v", err)
	}
	i.ProgressLogger.SetState(progress.StateComplete)
	return nil
}

// applyManifestSet applies a set of manifests to the cluster
func (i Installer) applyManifestSet(manifestSet manifest.ManifestSet) error {
	cname := string(manifestSet.Component)

	manifests := manifestSet.Manifests

	plog := i.ProgressLogger.NewComponent(cname)

	for _, obj := range manifests {
		obj, err := i.applyLabelsAndAnnotations(obj, cname)
		if err != nil {
			return err
		}
		if err := i.serverSideApply(obj); err != nil {
			plog.ReportError(err.Error())
			return err
		}
		plog.ReportProgress()
	}

	if !i.SkipWait {
		if err := WaitForResources(manifests, i.Kube, i.WaitTimeout, i.DryRun, plog); err != nil {
			werr := fmt.Errorf("failed to wait for resource: %v", err)
			plog.ReportError(werr.Error())
			return werr
		}
	}
	plog.ReportFinished()
	return nil
}

// serverSideApply creates or updates an object in the API server depending on whether it already exists.
func (i Installer) serverSideApply(obj manifest.Manifest) error {
	const fieldOwnerOperator = "istio-operator"
	dc, err := i.Kube.DynamicClientFor(obj.GroupVersionKind(), obj.Unstructured, "")
	if err != nil {
		return err
	}
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	var dryRun []string
	// TODO: can we do this a server-side dry run? it doesn't work well if the namespace is not already created
	if i.DryRun {
		return nil
	}
	if _, err := dc.Patch(context.TODO(), obj.GetName(), types.ApplyPatchType, []byte(obj.Content), metav1.PatchOptions{
		DryRun:       dryRun,
		Force:        ptr.Of(true),
		FieldManager: fieldOwnerOperator,
	}); err != nil {
		return fmt.Errorf("failed to update resource with server-side apply for obj %v: %v", objectStr, err)
	}
	return nil
}

func (i Installer) applyLabelsAndAnnotations(obj manifest.Manifest, cname string) (manifest.Manifest, error) {
	for k, v := range getOwnerLabels(i.Values, cname) {
		err := util.SetLabel(obj, k, v)
		if err != nil {
			return manifest.Manifest{}, err
		}
	}
	// We mutating the unstructured, must rebuild the YAML
	return manifest.FromObject(obj.Unstructured)
}

// prune removes resources that are in the cluster, but not a part of the currently installed set of objects.
func (i Installer) prune(manifests []manifest.ManifestSet) error {
	if i.DryRun {
		return nil
	}
	i.ProgressLogger.SetState(progress.StatePruning)

	// Build up a map of component->resources, so we know what to keep around
	excluded := map[component.Name]sets.String{}
	// Include all components in case we disabled some.
	for _, c := range component.AllComponents {
		excluded[c.UserFacingName] = sets.New[string]()
	}
	for _, mfs := range manifests {
		for _, m := range mfs.Manifests {
			excluded[mfs.Component].Insert(m.Hash())
		}
	}

	coreLabels := getOwnerLabels(i.Values, "")
	selector := klabels.Set(coreLabels).AsSelectorPreValidated()
	componentRequirement, err := klabels.NewRequirement(manifest.IstioComponentLabel, selection.Exists, nil)
	if err != nil {
		return err
	}
	selector = selector.Add(*componentRequirement)

	var errs util.Errors
	resources := uninstall.PrunedResourcesSchemas()
	for _, gvk := range resources {
		dc, err := i.Kube.DynamicClientFor(gvk, nil, "")
		if err != nil {
			return err
		}
		objs, err := dc.List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
		if err := controllers.IgnoreNotFound(err); err != nil {
			// Cluster may not even have these resources; ignore these errors
			return err
		}
		if objs == nil {
			continue
		}
		for component, excluded := range excluded {
			componentLabels := klabels.SelectorFromSet(getOwnerLabels(i.Values, string(component)))
			for _, obj := range objs.Items {
				if excluded.Contains(manifest.ObjectHash(&obj)) {
					continue
				}
				if obj.GetLabels()[manifest.OwningResourceNotPruned] == "true" {
					continue
				}
				// Label mismatch. Provided objects don't select against the component, so this likely means the object
				// is for another component.
				if !componentLabels.Matches(klabels.Set(obj.GetLabels())) {
					continue
				}

				if err := uninstall.DeleteResource(i.Kube, i.DryRun, i.Logger, &obj); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errs.ToError()
}

var componentDependencies = map[component.Name][]component.Name{
	component.PilotComponentName: {
		component.CNIComponentName,
		component.IngressComponentName,
		component.EgressComponentName,
	},
	component.BaseComponentName: {
		component.PilotComponentName,
	},
	component.CNIComponentName: {
		component.ZtunnelComponentName,
	},
}

func dependenciesChannels() map[component.Name]chan struct{} {
	ret := make(map[component.Name]chan struct{})
	for _, parent := range componentDependencies {
		for _, child := range parent {
			ret[child] = make(chan struct{}, 1)
		}
	}
	return ret
}

func getOwnerLabels(iop values.Map, c string) map[string]string {
	labels := make(map[string]string)

	labels[manifest.OperatorManagedLabel] = "Reconcile"
	labels[manifest.OperatorVersionLabel] = version.Info.Version
	if n := iop.GetPathString("metadata.name"); n != "" {
		labels[manifest.OwningResourceName] = n
	}
	if n := iop.GetPathString("metadata.namespace"); n != "" {
		labels[manifest.OwningResourceNamespace] = n
	}
	if n := iop.GetPathStringOr("spec.values.revision", "default"); n != "" {
		labels[label.IoIstioRev.Name] = n
	}

	if c != "" {
		labels[manifest.IstioComponentLabel] = c
	}
	return labels
}
