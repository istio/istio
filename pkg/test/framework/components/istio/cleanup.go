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
	"regexp"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/yml"
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
		for _, c := range i.ctx.AllClusters().Primaries().Kube() {
			i.cleanupCluster(c, &errG)
		}
		for _, c := range i.ctx.Clusters().Remotes().Kube() {
			i.cleanupCluster(c, &errG)
		}
		return errG.Wait().ErrorOrNil()
	}
	return nil
}

func (i *istioImpl) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Istio Deployment State...")
	ns := i.cfg.SystemNamespace
	d, err := ctx.CreateTmpDirectory("istio-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping Istio contents: %v", err)
		return
	}
	kube2.DumpPods(ctx, d, ns, []string{})
	kube2.DumpWebhooks(ctx, d)
	for _, c := range ctx.Clusters().Kube().Primaries() {
		kube2.DumpDebug(ctx, c, d, "configz")
		kube2.DumpDebug(ctx, c, d, "mcsz")
		kube2.DumpDebug(ctx, c, d, "clusterz")
	}
	// Dump istio-cni.
	kube2.DumpPods(ctx, d, "kube-system", []string{"k8s-app=istio-cni-node"})
}

func (i *istioImpl) cleanupCluster(c cluster.Cluster, errG *multierror.Group) {
	scopes.Framework.Infof("clean up cluster %s", c.Name())
	errG.Go(func() (err error) {
		if e := i.installer.Close(c); e != nil {
			err = multierror.Append(err, e)
		}
		// Cleanup all secrets and configmaps - these are dynamically created by tests and/or istiod so they are not captured above
		// This includes things like leader election locks (allowing next test to start without 30s delay),
		// custom cacerts, custom kubeconfigs, etc.
		// We avoid deleting the whole namespace since its extremely slow in Kubernetes (30-60s+)
		if e := c.Kube().CoreV1().Secrets(i.cfg.SystemNamespace).DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.Kube().CoreV1().ConfigMaps(i.cfg.SystemNamespace).DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		// Delete validating and mutating webhook configurations. These can be created outside of generated manifests
		// when installing with istioctl and must be deleted separately.
		if e := c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
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
