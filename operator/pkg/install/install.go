package install

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/operator/pkg/webhook"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/istiomultierror"
)

type Installer struct {
	Force          bool
	DryRun         bool
	SkipWait       bool
	Kube           kube.CLIClient
	WaitTimeout    time.Duration
	Logger         clog.Logger
	ProgressLogger *progress.Log
}

func (i Installer) install(manifests []manifest.ManifestSet) error {
	var mu sync.Mutex
	errors := istiomultierror.New()
	// wg waits for all manifest processing goroutines to finish
	var wg sync.WaitGroup

	dependencyWaitCh := dependenciesChannels()
	for _, manifest := range manifests {
		c := manifest.Component
		ms := manifest.Manifests
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s := dependencyWaitCh[c]; s != nil {
				<-s
			}

			if len(ms) != 0 {
				if err := i.ApplyManifest(manifest); err != nil {
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
	wg.Wait()
	return errors.ErrorOrNil()
}

func (i Installer) ApplyManifest(manifestSet manifest.ManifestSet) error {
	cname := string(manifestSet.Component)

	manifests := manifestSet.Manifests

	plog := i.ProgressLogger.NewComponent(cname)

	// TODO
	// allObjects.Sort(object.DefaultObjectOrder())
	for _, obj := range manifests {
		//if err := h.applyLabelsAndAnnotations(obju, cname); err != nil {
		//	return err
		//}
		if err := i.ServerSideApply(obj); err != nil {
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

// ServerSideApply creates or updates an object in the API server depending on whether it already exists.
func (i Installer) ServerSideApply(obj manifest.Manifest) error {
	const fieldOwnerOperator = "istio-operator"
	dc, err := i.Kube.DynamicClientFor(obj.GroupVersionKind(), obj.Unstructured, "")
	if err != nil {
		return err
	}
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	var dryRun []string
	// TODO: can we do this? it doesn't work well if the namespace is not already created
	if i.DryRun {
		return nil
		//	dryRun = []string{metav1.DryRunAll}
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

func (i Installer) InstallManifests(manifests []manifest.ManifestSet, values values.Map) error {
	// TODO: do not hardcode
	if err := util.CreateNamespace(i.Kube.Kube(), "istio-system", "", i.DryRun); err != nil {
		return err
	}

	if err := webhook.CheckWebhooks(manifests, values, i.Kube); err != nil {
		if i.Force {
			i.Logger.LogAndErrorf("invalid webhook configs; continuing because of --force: %v", err)
		} else {
			return err
		}
	}

	if err := i.install(manifests); err != nil {
		return err
	}

	i.ProgressLogger.SetState(progress.StateComplete)
	return nil
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
