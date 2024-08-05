package john

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/istiomultierror"
)

type Installer struct {
	force   bool
	dryRun  bool
	kube    kube.CLIClient
	timeout time.Duration
	l       clog.Logger
	pl      *progress.Log
}

func (i Installer) Install(manifests []ManifestSet) error {
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
			for _, ch := range ComponentDependencies[c] {
				dependencyWaitCh[ch] <- struct{}{}
			}
		}()
	}
	wg.Wait()
	return errors.ErrorOrNil()
}

func (i Installer) ApplyManifest(manifestSet ManifestSet) error {
	cname := string(manifestSet.Component)

	manifests := manifestSet.Manifests

	plog := i.pl.NewComponent(cname)

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

	if err := WaitForResources(manifests, i.kube, i.timeout, i.dryRun, plog); err != nil {
		werr := fmt.Errorf("failed to wait for resource: %v", err)
		plog.ReportError(werr.Error())
		return werr
	}
	plog.ReportFinished()
	return nil
}

// ServerSideApply creates or updates an object in the API server depending on whether it already exists.
func (i Installer) ServerSideApply(obj Manifest) error {
	const fieldOwnerOperator = "istio-operator"
	dc, err := i.kube.DynamicClientFor(obj.GroupVersionKind(), obj.Unstructured, "")
	if err != nil {
		return err
	}
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	var dryRun []string
	// TODO: can we do this? it doesn't work well if the namespace is not already created
	if i.dryRun {
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

func InstallManifests(manifests []ManifestSet, force bool, dryRun bool, kubeclient kube.CLIClient, timeout time.Duration, l clog.Logger) error {
	installer := Installer{
		force:   force,
		dryRun:  dryRun,
		kube:    kubeclient,
		timeout: timeout,
		l:       l,
		pl:      progress.NewLog(),
	}
	// TODO: do not hardcode
	if err := util.CreateNamespace(kubeclient.Kube(), "istio-system", "", dryRun); err != nil {
		return err
	}

	return installer.Install(manifests)
}

var ComponentDependencies = map[string][]string{
	"pilot": {
		"cni",
		"ingressGateways",
		"egressGateways",
	},
	"base": {
		"pilot",
	},
	"cni": {
		"ztunnel",
	},
}

func dependenciesChannels() map[string]chan struct{} {
	ret := make(map[string]chan struct{})
	for _, parent := range ComponentDependencies {
		for _, child := range parent {
			ret[child] = make(chan struct{}, 1)
		}
	}
	return ret
}
