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

package istio

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"time"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sync/errgroup"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
)

const (
	RetryDelay   = 2 * time.Second
	RetryTimeOut = 5 * time.Minute
)

func (i *istioImpl) Close() error {
	t0 := time.Now()
	scopes.Framework.Infof("=== BEGIN: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)

	// Write time spent for cleanup and deploy to ARTIFACTS/trace.yaml and logs to allow analyzing test times
	defer func() {
		delta := time.Since(t0)
		i.ctx.RecordTraceEvent("istio-cleanup", delta.Seconds())
		scopes.Framework.Infof("=== SUCCEEDED: Cleanup Istio in %v [Suite=%s] ===", delta, i.ctx.Settings().TestID)
	}()

	if i.cfg.DumpKubernetesManifests {
		i.installer.Dump(i.ctx)
	}

	if i.cfg.DeployIstio {
		errG := multierror.Group{}
		// Make sure to clean up primary clusters before remotes, or istiod will recreate some of the CMs that we delete
		// in the remote clusters before it's deleted.
		for _, c := range i.ctx.AllClusters().Primaries() {
			i.cleanupCluster(c, &errG)
		}
		for _, c := range i.ctx.Clusters().Remotes() {
			i.cleanupCluster(c, &errG)
		}
		return errG.Wait().ErrorOrNil()
	}

	// Execute External Control Plane Cleanup Script
	if i.cfg.ControlPlaneInstaller != "" && !i.cfg.DeployIstio {
		scopes.Framework.Infof("============= Execute Control Plane Cleanup =============")
		cmd := exec.Command(i.cfg.ControlPlaneInstaller, "cleanup", i.workDir)
		if err := cmd.Run(); err != nil {
			scopes.Framework.Errorf("failed to run external control plane installer: %v", err)
		}
	}

	for _, f := range i.istiod {
		f.Close()
	}
	return nil
}

func (i *istioImpl) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Istio Deployment State for %v...", ctx.ID())
	ns := i.cfg.SystemNamespace
	d, err := ctx.CreateTmpDirectory("istio-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping Istio contents: %v", err)
		return
	}
	g := errgroup.Group{}
	g.Go(func() error {
		kube2.DumpPods(ctx, d, ns, []string{})
		return nil
	})

	g.Go(func() error {
		kube2.DumpWebhooks(ctx, d)
		return nil
	})
	for _, c := range ctx.Clusters().Primaries() {
		g.Go(func() error {
			kube2.DumpDebug(ctx, c, d, "configz", ns)
			return nil
		})
		g.Go(func() error {
			kube2.DumpDebug(ctx, c, d, "mcsz", ns)
			return nil
		})
		g.Go(func() error {
			kube2.DumpDebug(ctx, c, d, "clusterz", ns)
			return nil
		})
	}
	// Dump kube-system as well.
	g.Go(func() error {
		kube2.DumpPods(ctx, d, "kube-system", []string{})
		return nil
	})
}

func (i *istioImpl) cleanupCluster(c cluster.Cluster, errG *multierror.Group) {
	scopes.Framework.Infof("clean up cluster %s", c.Name())

	// Tail `istio-cni` termination/shutdown logs, if any such pods present
	// in the system namespace
	label := "k8s-app=istio-cni-node"

	fetchFunc := kube2.NewPodFetch(c, i.cfg.SystemNamespace, label)

	fetched, e := fetchFunc()
	if e != nil {
		scopes.Framework.Infof("Failed retrieving pods: %v", e)
	}

	if len(fetched) != 0 {
		workDir, err := i.ctx.CreateTmpDirectory("istio-state")
		if err != nil {
			scopes.Framework.Errorf("Unable to create directory for dumping istio-cni termlogs: %v", err)
			return
		}
		for _, pod := range fetched {
			go kube2.DumpTerminationLogs(context.Background(), c, workDir, pod, "install-cni")
		}
	}

	errG.Go(func() (err error) {
		if e := i.installer.Close(c); e != nil {
			err = multierror.Append(err, e)
		}

		// Cleanup all secrets and configmaps - these are dynamically created by tests and/or istiod so they are not captured above
		// This includes things like leader election locks (allowing next test to start without 30s delay),
		// custom cacerts, custom kubeconfigs, etc.
		// We avoid deleting the whole namespace since its extremely slow in Kubernetes (30-60s+)
		if e := c.Kube().CoreV1().Secrets(i.cfg.SystemNamespace).DeleteCollection(
			context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.Kube().CoreV1().ConfigMaps(i.cfg.SystemNamespace).DeleteCollection(
			context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		// Delete validating and mutating webhook configurations. These can be created outside of generated manifests
		// when installing with istioctl and must be deleted separately.
		if e := c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().DeleteCollection(
			context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().DeleteCollection(
			context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}

		// Delete the "default" revision tag service that was created similarly to MutatingWebhookConfigurations
		if e := c.Kube().CoreV1().Services(i.cfg.SystemNamespace).Delete(
			context.Background(), "istiod-revision-tag-default", metav1.DeleteOptions{}); e != nil {
			// Only append the error if it's not a NotFound error
			if !kerrors.IsNotFound(e) {
				err = multierror.Append(err, e)
			}
		}

		// We deleted all resources, but don't report cleanup finished until all Istio pods
		// in the system namespace have actually terminated.
		cleanErr := retry.UntilSuccess(func() error {
			label := "app.kubernetes.io/part-of=istio"

			fetchPodFunc := kube2.NewPodFetch(c, i.cfg.SystemNamespace, label)

			fetchedPod, e := fetchPodFunc()
			if e != nil {
				scopes.Framework.Infof("Failed retrieving pods: %v", e)
			}

			// In Openshift if takes time to cleanup the services.
			// Lets check for the services cleanup as well.
			fetchSvcFunc := kube2.NewServiceFetch(c, i.cfg.SystemNamespace, label)

			fetchedSvc, e := fetchSvcFunc()
			if e != nil {
				scopes.Framework.Infof("Failed retrieving services: %v", e)
			}

			if len(fetchedPod) == 0 && len(fetchedSvc) == 0 {
				return nil
			}
			res := fmt.Sprintf("Still waiting for %d pods and %d services to terminate in %s ", len(fetchedPod), len(fetchedSvc), i.cfg.SystemNamespace)
			scopes.Framework.Infof(res)

			for _, svc := range fetchedSvc {
				scopes.Framework.Infof("Service still present: namespace=%s, name=%s", svc.Namespace, svc.Name)
			}

			return errors.New(res)
		}, retry.Timeout(RetryTimeOut), retry.Delay(RetryDelay))

		err = multierror.Append(err, cleanErr)

		return
	})
}

// When we cleanup, we should not delete CRDs. This will filter out all the crds
func removeCRDs(istioYaml string) string {
	allParts := yml.SplitString(istioYaml)
	nonCrds := make([]string, 0, len(allParts))

	// Make the regular expression multi-line and anchor to the beginning of the line.
	r := regexp.MustCompile(`(?m)^kind: CustomResourceDefinition$`)

	for _, p := range allParts {
		if r.MatchString(p) {
			continue
		}
		nonCrds = append(nonCrds, p)
	}

	return yml.JoinString(nonCrds...)
}
