package ownerreference

import (
	"context"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"istio.io/istio/pilot/pkg/features"
	alifeatures "istio.io/istio/pkg/ali/features"
	"istio.io/istio/pkg/log"
)

var (
	doOnce sync.Once

	kubeClient client.Client

	uid types.UID

	empty metav1.OwnerReference
)

func GetOwnerResourceUID() {
	var err error
	kubeClient, err = client.New(config.GetConfigOrDie(), client.Options{})
	if err != nil {
		panic(err)
	}

	var resource unstructured.Unstructured
	resource.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.kruise.io",
		Kind:    "CloneSet",
		Version: "v1alpha1",
	})
	key := client.ObjectKey{
		Namespace: alifeatures.WatchResourcesByNamespaceForPrimaryCluster,
		Name:      features.ClusterName,
	}
	if err = kubeClient.Get(context.TODO(), key, &resource); err != nil {
		log.Errorf("get owner resource fail, err: %v", err)
		return
	}

	uid = resource.GetUID()
	log.Infof("owner uid is %s", uid)
}

func GenOwnerReference() metav1.OwnerReference {
	doOnce.Do(GetOwnerResourceUID)

	if uid == "" {
		return empty
	}

	return metav1.OwnerReference{
		APIVersion: "apps.kruise.io/v1alpha1",
		Kind:       "CloneSet",
		Name:       features.ClusterName,
		UID:        uid,
	}
}
