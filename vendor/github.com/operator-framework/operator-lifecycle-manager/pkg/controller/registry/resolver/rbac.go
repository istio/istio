package resolver

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/controller/install"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
)

var generateName = func(base string) string {
	return names.SimpleNameGenerator.GenerateName(base + "-")
}

type OperatorPermissions struct {
	ServiceAccount      *corev1.ServiceAccount
	Roles               []*rbacv1.Role
	RoleBindings        []*rbacv1.RoleBinding
	ClusterRoles        []*rbacv1.ClusterRole
	ClusterRoleBindings []*rbacv1.ClusterRoleBinding
}

func NewOperatorPermissions(serviceAccount *corev1.ServiceAccount) *OperatorPermissions {
	return &OperatorPermissions{
		ServiceAccount:      serviceAccount,
		Roles:               []*rbacv1.Role{},
		RoleBindings:        []*rbacv1.RoleBinding{},
		ClusterRoles:        []*rbacv1.ClusterRole{},
		ClusterRoleBindings: []*rbacv1.ClusterRoleBinding{},
	}
}

func (o *OperatorPermissions) AddRole(role *rbacv1.Role) {
	o.Roles = append(o.Roles, role)
}

func (o *OperatorPermissions) AddRoleBinding(roleBinding *rbacv1.RoleBinding) {
	o.RoleBindings = append(o.RoleBindings, roleBinding)
}

func (o *OperatorPermissions) AddClusterRole(clusterRole *rbacv1.ClusterRole) {
	o.ClusterRoles = append(o.ClusterRoles, clusterRole)
}

func (o *OperatorPermissions) AddClusterRoleBinding(clusterRoleBinding *rbacv1.ClusterRoleBinding) {
	o.ClusterRoleBindings = append(o.ClusterRoleBindings, clusterRoleBinding)
}

func RBACForClusterServiceVersion(csv *v1alpha1.ClusterServiceVersion) (map[string]*OperatorPermissions, error) {
	permissions := map[string]*OperatorPermissions{}

	// Use a StrategyResolver to get the strategy details
	strategyResolver := install.StrategyResolver{}
	strategy, err := strategyResolver.UnmarshalStrategy(csv.Spec.InstallStrategy)
	if err != nil {
		return nil, err
	}

	// Assume the strategy is for a deployment
	strategyDetailsDeployment, ok := strategy.(*install.StrategyDetailsDeployment)
	if !ok {
		return nil, fmt.Errorf("could not assert strategy implementation as deployment for CSV %s", csv.GetName())
	}

	// Resolve Permissions
	for _, permission := range strategyDetailsDeployment.Permissions {
		// Create ServiceAccount if necessary
		if _, ok := permissions[permission.ServiceAccountName]; !ok {
			serviceAccount := &corev1.ServiceAccount{}
			serviceAccount.SetName(permission.ServiceAccountName)
			ownerutil.AddNonBlockingOwner(serviceAccount, csv)

			permissions[permission.ServiceAccountName] = NewOperatorPermissions(serviceAccount)
		}

		// Create Role
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:            generateName(csv.GetName()),
				Namespace:       csv.GetNamespace(),
				OwnerReferences: []metav1.OwnerReference{ownerutil.NonBlockingOwner(csv)},
				Labels:          ownerutil.OwnerLabel(csv),
			},
			Rules: permission.Rules,
		}
		permissions[permission.ServiceAccountName].AddRole(role)

		// Create RoleBinding
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            generateName(fmt.Sprintf("%s-%s", role.GetName(), permission.ServiceAccountName)),
				Namespace:       csv.GetNamespace(),
				OwnerReferences: []metav1.OwnerReference{ownerutil.NonBlockingOwner(csv)},
				Labels:          ownerutil.OwnerLabel(csv),
			},
			RoleRef: rbacv1.RoleRef{
				Kind:     "Role",
				Name:     role.GetName(),
				APIGroup: rbacv1.GroupName},
			Subjects: []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      permission.ServiceAccountName,
				Namespace: csv.GetNamespace(),
			}},
		}
		permissions[permission.ServiceAccountName].AddRoleBinding(roleBinding)
	}

	// Resolve ClusterPermissions as StepResources
	for _, permission := range strategyDetailsDeployment.ClusterPermissions {
		// Create ServiceAccount if necessary
		if _, ok := permissions[permission.ServiceAccountName]; !ok {
			serviceAccount := &corev1.ServiceAccount{}
			serviceAccount.SetName(permission.ServiceAccountName)
			ownerutil.AddNonBlockingOwner(serviceAccount, csv)

			permissions[permission.ServiceAccountName] = NewOperatorPermissions(serviceAccount)
		}

		// Create ClusterRole
		role := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:            generateName(csv.GetName()),
				OwnerReferences: []metav1.OwnerReference{ownerutil.NonBlockingOwner(csv)},
				Labels:          ownerutil.OwnerLabel(csv),
			},
			Rules: permission.Rules,
		}
		permissions[permission.ServiceAccountName].AddClusterRole(role)

		// Create ClusterRoleBinding
		roleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            generateName(fmt.Sprintf("%s-%s", role.GetName(), permission.ServiceAccountName)),
				Namespace:       csv.GetNamespace(),
				OwnerReferences: []metav1.OwnerReference{ownerutil.NonBlockingOwner(csv)},
				Labels:          ownerutil.OwnerLabel(csv),
			},
			RoleRef: rbacv1.RoleRef{
				Kind:     "ClusterRole",
				Name:     role.GetName(),
				APIGroup: rbacv1.GroupName,
			},
			Subjects: []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      permission.ServiceAccountName,
				Namespace: csv.GetNamespace(),
			}},
		}
		permissions[permission.ServiceAccountName].AddClusterRoleBinding(roleBinding)
	}
	return permissions, nil
}
