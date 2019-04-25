package resolver

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/operator-framework/operator-registry/pkg/registry"
	extScheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	k8sscheme.AddToScheme(scheme)
	extScheme.AddToScheme(scheme)
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// NewStepResourceForObject returns a new StepResource for the provided object
func NewStepResourceFromObject(obj runtime.Object, catalogSourceName, catalogSourceNamespace string) (v1alpha1.StepResource, error) {
	var resource v1alpha1.StepResource

	// set up object serializer
	serializer := k8sjson.NewSerializer(k8sjson.DefaultMetaFactory, scheme, scheme, false)

	// create an object manifest
	var manifest bytes.Buffer
	err := serializer.Encode(obj, &manifest)
	if err != nil {
		return resource, err
	}

	if err := ownerutil.InferGroupVersionKind(obj); err != nil {
		return resource, err
	}

	gvk := obj.GetObjectKind().GroupVersionKind()

	metaObj, ok := obj.(metav1.Object)
	if !ok {
		return resource, fmt.Errorf("couldn't get object metadata")
	}

	name := metaObj.GetName()
	if name == "" {
		name = metaObj.GetGenerateName()
	}

	// create the resource
	resource = v1alpha1.StepResource{
		Name:                   name,
		Kind:                   gvk.Kind,
		Group:                  gvk.Group,
		Version:                gvk.Version,
		Manifest:               manifest.String(),
		CatalogSource:          catalogSourceName,
		CatalogSourceNamespace: catalogSourceNamespace,
	}

	return resource, nil
}

func NewSubscriptionStepResource(namespace string, info OperatorSourceInfo) (v1alpha1.StepResource, error) {
	return NewStepResourceFromObject(&v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      strings.Join([]string{info.Package, info.Channel, info.Catalog.Name, info.Catalog.Namespace}, "-"),
		},
		Spec: &v1alpha1.SubscriptionSpec{
			CatalogSource:          info.Catalog.Name,
			CatalogSourceNamespace: info.Catalog.Namespace,
			Package:                info.Package,
			Channel:                info.Channel,
			InstallPlanApproval:    v1alpha1.ApprovalAutomatic,
		},
	}, info.Catalog.Name, info.Catalog.Namespace)
}

func NewStepResourceFromBundle(bundle *registry.Bundle, namespace, catalogSourceName, catalogSourceNamespace string) ([]v1alpha1.StepResource, error) {
	steps := []v1alpha1.StepResource{}

	csv, err := bundle.ClusterServiceVersion()
	if err != nil {
		return nil, err
	}

	csv.SetNamespace(namespace)

	for _, object := range bundle.Objects {
		step, err := NewStepResourceFromObject(object, catalogSourceName, catalogSourceNamespace)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	operatorServiceAccountSteps, err := NewServiceAccountStepResources(csv, catalogSourceName, catalogSourceNamespace)
	if err != nil {
		return nil, err
	}
	steps = append(steps, operatorServiceAccountSteps...)
	return steps, nil
}

// NewServiceAccountStepResources returns a list of step resources required to satisfy the RBAC requirements of the given CSV's InstallStrategy
func NewServiceAccountStepResources(csv *v1alpha1.ClusterServiceVersion, catalogSourceName, catalogSourceNamespace string) ([]v1alpha1.StepResource, error) {
	var rbacSteps []v1alpha1.StepResource

	operatorPermissions, err := RBACForClusterServiceVersion(csv)
	if err != nil {
		return nil, err
	}

	for _, perms := range operatorPermissions {
		step, err := NewStepResourceFromObject(perms.ServiceAccount, catalogSourceName, catalogSourceNamespace)
		if err != nil {
			return nil, err
		}
		rbacSteps = append(rbacSteps, step)
		for _, role := range perms.Roles {
			step, err := NewStepResourceFromObject(role, catalogSourceName, catalogSourceNamespace)
			if err != nil {
				return nil, err
			}
			rbacSteps = append(rbacSteps, step)
		}
		for _, roleBinding := range perms.RoleBindings {
			step, err := NewStepResourceFromObject(roleBinding, catalogSourceName, catalogSourceNamespace)
			if err != nil {
				return nil, err
			}
			rbacSteps = append(rbacSteps, step)
		}
		for _, clusterRole := range perms.ClusterRoles {
			step, err := NewStepResourceFromObject(clusterRole, catalogSourceName, catalogSourceNamespace)
			if err != nil {
				return nil, err
			}
			rbacSteps = append(rbacSteps, step)
		}
		for _, clusterRoleBinding := range perms.ClusterRoleBindings {
			step, err := NewStepResourceFromObject(clusterRoleBinding, catalogSourceName, catalogSourceNamespace)
			if err != nil {
				return nil, err
			}
			rbacSteps = append(rbacSteps, step)
		}
	}
	return rbacSteps, nil
}
