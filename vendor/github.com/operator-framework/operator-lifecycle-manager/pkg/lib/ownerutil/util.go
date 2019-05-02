package ownerutil

import (
	"fmt"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha2"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	OwnerKey          = "olm.owner"
	OwnerNamespaceKey = "olm.owner.namespace"
)

var (
	NotController          = false
	DontBlockOwnerDeletion = false
)

// Owner is used to build an OwnerReference, and we need type and object metadata
type Owner interface {
	metav1.Object
	runtime.Object
}

func IsOwnedBy(object metav1.Object, owner Owner) bool {
	for _, oref := range object.GetOwnerReferences() {
		if oref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

func IsOwnedByKind(object metav1.Object, ownerKind string) bool {
	for _, oref := range object.GetOwnerReferences() {
		if oref.Kind == ownerKind {
			return true
		}
	}
	return false
}

func GetOwnerByKind(object metav1.Object, ownerKind string) *metav1.OwnerReference {
	for _, oref := range object.GetOwnerReferences() {
		if oref.Kind == ownerKind {
			return &oref
		}
	}
	return nil
}

// GetOwnersByKind returns all OwnerReferences of the given kind listed by the given object
func GetOwnersByKind(object metav1.Object, ownerKind string) []metav1.OwnerReference {
	var orefs []metav1.OwnerReference
	for _, oref := range object.GetOwnerReferences() {
		if oref.Kind == ownerKind {
			orefs = append(orefs, oref)
		}
	}
	return orefs
}

// HasOwnerConflict checks if the given list of OwnerReferences points to owners other than the target.
// This function returns true if the list of OwnerReferences is empty or contains elements of the same kind as
// the target but does not include the target OwnerReference itself. This function returns false if the list contains
// the target, or has no elements of the same kind as the target.
//
// Note: This is imporant when determining if a Role, RoleBinding, ClusterRole, or ClusterRoleBinding
// can be used to satisfy permissions of a CSV. If the target CSV is not a member of the RBAC resource's
// OwnerReferences, then we know the resource can be garbage collected by OLM independently of the target
// CSV
func HasOwnerConflict(target Owner, owners []metav1.OwnerReference) bool {
	// Infer TypeMeta for the target
	if err := InferGroupVersionKind(target); err != nil {
		log.Warn(err.Error())
	}

	conflicts := false
	for _, owner := range owners {
		gvk := target.GetObjectKind().GroupVersionKind()
		if owner.Kind == gvk.Kind && owner.APIVersion == gvk.Version {
			if owner.Name == target.GetName() && owner.UID == target.GetUID() {
				return false
			}

			conflicts = true
		}
	}

	return conflicts
}

// Adoptable checks whether a resource with the given set of OwnerReferences is "adoptable" by
// the target OwnerReference. This function returns true if there exists an element in owners
// referencing the same kind target does, otherwise it returns false.
func Adoptable(target Owner, owners []metav1.OwnerReference) bool {
	if len(owners) == 0 {
		// Resources with no owners are not adoptable
		return false
	}

	// Infer TypeMeta for the target
	if err := InferGroupVersionKind(target); err != nil {
		log.Warn(err.Error())
	}

	for _, owner := range owners {
		gvk := target.GetObjectKind().GroupVersionKind()
		if owner.Kind == gvk.Kind {
			return true
		}
	}

	return false
}

// AddNonBlockingOwner adds a nonblocking owner to the ownerref list.
func AddNonBlockingOwner(object metav1.Object, owner Owner) {
	ownerRefs := object.GetOwnerReferences()
	if ownerRefs == nil {
		ownerRefs = []metav1.OwnerReference{}
	}
	ownerRefs = append(ownerRefs, NonBlockingOwner(owner))
	object.SetOwnerReferences(ownerRefs)
}

// NonBlockingOwner returns an ownerrefence to be added to an ownerref list
func NonBlockingOwner(owner Owner) metav1.OwnerReference {
	// Most of the time we won't have TypeMeta on the object, so we infer it for types we know about
	if err := InferGroupVersionKind(owner); err != nil {
		log.Warn(err.Error())
	}

	gvk := owner.GetObjectKind().GroupVersionKind()
	apiVersion, kind := gvk.ToAPIVersionAndKind()

	return metav1.OwnerReference{
		APIVersion:         apiVersion,
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		BlockOwnerDeletion: &DontBlockOwnerDeletion,
		Controller:         &NotController,
	}
}

// OwnerLabel returns a label added to generated objects for later querying
func OwnerLabel(owner metav1.Object) map[string]string {
	return map[string]string{
		OwnerKey:          owner.GetName(),
		OwnerNamespaceKey: owner.GetNamespace(),
	}
}

// OwnerQuery returns a label selector to find generated objects owned by owner
func CSVOwnerSelector(owner *v1alpha1.ClusterServiceVersion) labels.Selector {
	return labels.SelectorFromSet(OwnerLabel(owner))
}

// AddOwner adds an owner to the ownerref list.
func AddOwner(object metav1.Object, owner Owner, blockOwnerDeletion, isController bool) {
	// Most of the time we won't have TypeMeta on the object, so we infer it for types we know about
	if err := InferGroupVersionKind(owner); err != nil {
		log.Warn(err.Error())
	}

	ownerRefs := object.GetOwnerReferences()
	if ownerRefs == nil {
		ownerRefs = []metav1.OwnerReference{}
	}
	gvk := owner.GetObjectKind().GroupVersionKind()
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	ownerRefs = append(ownerRefs, metav1.OwnerReference{
		APIVersion:         apiVersion,
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		BlockOwnerDeletion: &blockOwnerDeletion,
		Controller:         &isController,
	})
	object.SetOwnerReferences(ownerRefs)
}

// EnsureOwner adds a new owner if needed and returns whether the object already had the owner.
func EnsureOwner(object metav1.Object, owner Owner) bool {
	if IsOwnedBy(object, owner) {
		return true
	} else {
		AddNonBlockingOwner(object, owner)
		return false
	}
}

// InferGroupVersionKind adds TypeMeta to an owner so that it can be written to an ownerref.
// TypeMeta is generally only known at serialization time, so we often won't know what GVK an owner has.
// For the types we know about, we can add the GVK of the apis that we're using the interact with the object.
func InferGroupVersionKind(obj runtime.Object) error {
	objectKind := obj.GetObjectKind()
	if !objectKind.GroupVersionKind().Empty() {
		// objectKind already has TypeMeta, no inference needed
		return nil
	}

	switch obj.(type) {
	case *corev1.Service:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Service",
		})
	case *corev1.ServiceAccount:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ServiceAccount",
		})
	case *rbac.ClusterRole:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "rbac.authorization.k8s.io",
			Version: "v1",
			Kind:    "ClusterRole",
		})
	case *rbac.ClusterRoleBinding:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "rbac.authorization.k8s.io",
			Version: "v1",
			Kind:    "ClusterRoleBinding",
		})
	case *rbac.Role:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "rbac.authorization.k8s.io",
			Version: "v1",
			Kind:    "Role",
		})
	case *rbac.RoleBinding:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "rbac.authorization.k8s.io",
			Version: "v1",
			Kind:    "RoleBinding",
		})
	case *v1alpha1.ClusterServiceVersion:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha1.GroupName,
			Version: v1alpha1.GroupVersion,
			Kind:    v1alpha1.ClusterServiceVersionKind,
		})
	case *v1alpha1.InstallPlan:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha1.GroupName,
			Version: v1alpha1.GroupVersion,
			Kind:    v1alpha1.InstallPlanKind,
		})
	case *v1alpha1.Subscription:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha1.GroupName,
			Version: v1alpha1.GroupVersion,
			Kind:    v1alpha1.SubscriptionKind,
		})
	case *v1alpha1.CatalogSource:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha1.GroupName,
			Version: v1alpha1.GroupVersion,
			Kind:    v1alpha1.CatalogSourceKind,
		})
	case *v1alpha2.OperatorGroup:
		objectKind.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha2.GroupName,
			Version: v1alpha2.GroupVersion,
			Kind:    "OperatorGroup",
		})
	default:
		return fmt.Errorf("could not infer GVK for object: %#v, %#v", obj, objectKind)
	}
	return nil
}
