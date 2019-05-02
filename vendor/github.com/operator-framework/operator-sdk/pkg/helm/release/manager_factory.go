// Copyright 2018 The Operator-SDK Authors
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

package release

import (
	"fmt"
	"strings"

	"github.com/martinlindhe/base36"
	"github.com/pborman/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apitypes "k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	helmengine "k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/kube"
	"k8s.io/helm/pkg/storage"
	"k8s.io/helm/pkg/storage/driver"
	"k8s.io/helm/pkg/tiller"
	"k8s.io/helm/pkg/tiller/environment"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/operator-framework/operator-sdk/pkg/helm/client"
	"github.com/operator-framework/operator-sdk/pkg/helm/engine"
	"github.com/operator-framework/operator-sdk/pkg/helm/internal/types"
)

// ManagerFactory creates Managers that are specific to custom resources. It is
// used by the HelmOperatorReconciler during resource reconciliation, and it
// improves decoupling between reconciliation logic and the Helm backend
// components used to manage releases.
type ManagerFactory interface {
	NewManager(r *unstructured.Unstructured) (Manager, error)
}

type managerFactory struct {
	mgr      crmanager.Manager
	chartDir string
}

// NewManagerFactory returns a new Helm manager factory capable of installing and uninstalling releases.
func NewManagerFactory(mgr crmanager.Manager, chartDir string) ManagerFactory {
	return &managerFactory{mgr, chartDir}
}

func (f managerFactory) NewManager(cr *unstructured.Unstructured) (Manager, error) {
	clientv1, err := v1.NewForConfig(f.mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get core/v1 client: %s", err)
	}
	storageBackend := storage.Init(driver.NewSecrets(clientv1.Secrets(cr.GetNamespace())))
	tillerKubeClient, err := client.NewFromManager(f.mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to get client from manager: %s", err)
	}
	releaseServer, err := getReleaseServer(cr, storageBackend, tillerKubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get helm release server: %s", err)
	}
	return &manager{
		storageBackend:   storageBackend,
		tillerKubeClient: tillerKubeClient,
		chartDir:         f.chartDir,

		tiller:      releaseServer,
		releaseName: getReleaseName(cr),
		namespace:   cr.GetNamespace(),

		spec:   cr.Object["spec"],
		status: types.StatusFor(cr),
	}, nil
}

// getReleaseServer creates a ReleaseServer configured with a rendering engine that adds ownerrefs to rendered assets
// based on the CR.
func getReleaseServer(cr *unstructured.Unstructured, storageBackend *storage.Storage, tillerKubeClient *kube.Client) (*tiller.ReleaseServer, error) {
	controllerRef := metav1.NewControllerRef(cr, cr.GroupVersionKind())
	ownerRefs := []metav1.OwnerReference{
		*controllerRef,
	}
	baseEngine := helmengine.New()
	e := engine.NewOwnerRefEngine(baseEngine, ownerRefs)
	var ey environment.EngineYard = map[string]environment.Engine{
		environment.GoTplEngine: e,
	}
	env := &environment.Environment{
		EngineYard: ey,
		Releases:   storageBackend,
		KubeClient: tillerKubeClient,
	}
	kubeconfig, err := tillerKubeClient.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	cs, err := clientset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return tiller.NewReleaseServer(env, cs, false), nil
}

func getReleaseName(cr *unstructured.Unstructured) string {
	return fmt.Sprintf("%s-%s", cr.GetName(), shortenUID(cr.GetUID()))
}

func shortenUID(uid apitypes.UID) string {
	u := uuid.Parse(string(uid))
	uidBytes, err := u.MarshalBinary()
	if err != nil {
		return strings.Replace(string(uid), "-", "", -1)
	}
	return strings.ToLower(base36.EncodeBytes(uidBytes))
}
