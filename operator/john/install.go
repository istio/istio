package john

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/istiomultierror"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"
)

type Installer struct {
	force bool
	dryRun bool
	ic kube.CLIClient
	kc client.Client
	timeout time.Duration
	l clog.Logger
	pl *progress.Log
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

func (i Installer) ApplyManifest(manifestSet ManifestSet) error{

	cname := string(manifestSet.Component)

	manifests := manifestSet.Manifests

	plog := i.pl.NewComponent(cname)


	// TODO
	//allObjects.Sort(object.DefaultObjectOrder())
	for _, obj := range manifests {
		//if err := h.applyLabelsAndAnnotations(obju, cname); err != nil {
		//	return err
		//}
		if err := i.ServerSideApply(obj.Unstructured); err != nil {
			plog.ReportError(err.Error())
			return err
		}
		plog.ReportProgress()
	}

	if err := WaitForResources(manifests, i.ic, i.timeout, i.dryRun, plog); err != nil {
		werr := fmt.Errorf("failed to wait for resource: %v", err)
		plog.ReportError(werr.Error())
		return werr
	}
	plog.ReportFinished()
	return nil
}
// ServerSideApply creates or updates an object in the API server depending on whether it already exists.
func (i Installer) ServerSideApply(obj *unstructured.Unstructured) error {
	const fieldOwnerOperator = "istio-operator"
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	opts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(fieldOwnerOperator)}
	if err := i.kc.Patch(context.TODO(), obj, client.Apply, opts...); err != nil {
		return fmt.Errorf("failed to update resource with server-side apply for obj %v: %v", objectStr, err)
	}
	return nil
}

func InstallManifests(manifests []ManifestSet, force bool, dryRun bool, ic kube.CLIClient, kc client.Client, timeout time.Duration, l clog.Logger) error {
	installer := Installer{
		force: force,
		dryRun: dryRun,
		ic: ic,
		kc: kc,
		timeout: timeout,
		l: l,
		pl: progress.NewLog(),
	}
	// TODO: do not hardcode
	if err := util.CreateNamespace(ic.Kube(), "istio-system", "", dryRun); err != nil {
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
