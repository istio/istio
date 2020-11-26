// +build integ
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

package operator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	IstioNamespace    = "istio-system"
	OperatorNamespace = "istio-operator"
	retryDelay        = time.Second
	retryTimeOut      = 20 * time.Minute
)

var (
	// ManifestPath is path of local manifests which istioctl operator init refers to.
	ManifestPath = filepath.Join(env.IstioSrc, "manifests")
	// ManifestPathContainer is path of manifests in the operator container for controller to work with.
	ManifestPathContainer = "/var/lib/istio/manifests"
	iopCRFile             = ""
)

func cleanupInClusterCRs(t *testing.T, cs resource.Cluster) {
	// clean up hanging installed-state CR from previous tests
	gvr := schema.GroupVersionResource{
		Group:    "install.istio.io",
		Version:  "v1alpha1",
		Resource: "istiooperators",
	}
	if err := cs.Dynamic().Resource(gvr).Namespace(IstioNamespace).Delete(context.TODO(),
		"installed-state", kubeApiMeta.DeleteOptions{}); err != nil {
		t.Logf(err.Error())
	}
}
