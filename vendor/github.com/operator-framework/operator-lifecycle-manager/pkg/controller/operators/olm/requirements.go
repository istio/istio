package olm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/coreos/go-semver/semver"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	olmErrors "github.com/operator-framework/operator-lifecycle-manager/pkg/controller/errors"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/controller/install"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *Operator) minKubeVersionStatus(name string, minKubeVersion string) (met bool, statuses []v1alpha1.RequirementStatus) {
	status := v1alpha1.RequirementStatus{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
		Name:    name,
	}

	if minKubeVersion == "" {
		status.Status = v1alpha1.RequirementStatusReasonNotPresent
		status.Message = "CSV missing minimum kube version specification"
		met = true
		statuses = append(statuses, status)
		return
	}

	// Retrieve server k8s version
	serverVersionInfo, err := a.OpClient.KubernetesInterface().Discovery().ServerVersion()
	if err != nil {
		status.Status = v1alpha1.RequirementStatusReasonPresentNotSatisfied
		status.Message = "Server version discovery error"
		met = false
		statuses = append(statuses, status)
		return
	}

	serverVersion, err := semver.NewVersion(strings.Split(strings.TrimPrefix(serverVersionInfo.String(), "v"), "-")[0])
	if err != nil {
		status.Status = v1alpha1.RequirementStatusReasonPresentNotSatisfied
		status.Message = "Server version parsing error"
		met = false
		statuses = append(statuses, status)
		return
	}

	csvVersionInfo, err := semver.NewVersion(minKubeVersion)
	if err != nil {
		status.Status = v1alpha1.RequirementStatusReasonPresentNotSatisfied
		status.Message = "CSV version parsing error"
		met = false
		statuses = append(statuses, status)
		return
	}

	if csvVersionInfo.Compare(*serverVersion) > 0 {
		status.Status = v1alpha1.RequirementStatusReasonPresentNotSatisfied
		status.Message = fmt.Sprintf("CSV version requirement not met: minKubeVersion (%s) > server version (%s)", minKubeVersion, serverVersion.String())
		met = false
		statuses = append(statuses, status)
		return
	}

	status.Status = v1alpha1.RequirementStatusReasonPresent
	status.Message = fmt.Sprintf("CSV minKubeVersion (%s) less than server version (%s)", minKubeVersion, serverVersionInfo.String())
	met = true
	statuses = append(statuses, status)
	return
}

func (a *Operator) requirementStatus(strategyDetailsDeployment *install.StrategyDetailsDeployment, crdDescs []v1alpha1.CRDDescription,
	ownedAPIServiceDescs []v1alpha1.APIServiceDescription, requiredAPIServiceDescs []v1alpha1.APIServiceDescription,
	requiredNativeAPIs []metav1.GroupVersionKind) (met bool, statuses []v1alpha1.RequirementStatus) {
	met = true

	// Check for CRDs
	for _, r := range crdDescs {
		status := v1alpha1.RequirementStatus{
			Group:   "apiextensions.k8s.io",
			Version: "v1beta1",
			Kind:    "CustomResourceDefinition",
			Name:    r.Name,
		}

		// check if CRD exists - this verifies group, version, and kind, so no need for GVK check via discovery
		crd, err := a.lister.APIExtensionsV1beta1().CustomResourceDefinitionLister().Get(r.Name)
		if err != nil {
			status.Status = v1alpha1.RequirementStatusReasonNotPresent
			status.Message = "CRD is not present"
			a.Log.Debugf("Setting 'met' to false, %v with status %v, with err: %v", r.Name, status, err)
			met = false
			statuses = append(statuses, status)
			continue
		}

		if crd.Spec.Version != r.Version {
			served := false
			for _, version := range crd.Spec.Versions {
				if version.Name == r.Version {
					if version.Served {
						served = true
					}
					break
				}
			}

			if !served {
				status.Status = v1alpha1.RequirementStatusReasonNotPresent
				status.Message = "CRD version not served"
				a.Log.Debugf("Setting 'met' to false, %v with status %v, CRD version %v not found", r.Name, status, r.Version)
				met = false
				statuses = append(statuses, status)
				continue
			}
		}

		// Check if CRD has successfully registered with k8s API
		established := false
		namesAccepted := false
		for _, cdt := range crd.Status.Conditions {
			switch cdt.Type {
			case v1beta1.Established:
				if cdt.Status == v1beta1.ConditionTrue {
					established = true
				}
			case v1beta1.NamesAccepted:
				if cdt.Status == v1beta1.ConditionTrue {
					namesAccepted = true
				}
			}
		}

		if established && namesAccepted {
			status.Status = v1alpha1.RequirementStatusReasonPresent
			status.Message = "CRD is present and Established condition is true"
			status.UUID = string(crd.GetUID())
			statuses = append(statuses, status)
		} else {
			status.Status = v1alpha1.RequirementStatusReasonNotAvailable
			status.Message = "CRD is present but the Established condition is False (not available)"
			met = false
			a.Log.Debugf("Setting 'met' to false, %v with status %v, established=%v, namesAccepted=%v", r.Name, status, established, namesAccepted)
			statuses = append(statuses, status)
		}
	}

	// Check for required API services
	for _, r := range requiredAPIServiceDescs {
		name := fmt.Sprintf("%s.%s", r.Version, r.Group)
		status := v1alpha1.RequirementStatus{
			Group:   "apiregistration.k8s.io",
			Version: "v1",
			Kind:    "APIService",
			Name:    name,
		}

		// Check if GVK exists
		if err := a.isGVKRegistered(r.Group, r.Version, r.Kind); err != nil {
			status.Status = "NotPresent"
			met = false
			statuses = append(statuses, status)
			continue
		}

		// Check if APIService is registered
		apiService, err := a.lister.APIRegistrationV1().APIServiceLister().Get(name)
		if err != nil {
			status.Status = "NotPresent"
			met = false
			statuses = append(statuses, status)
			continue
		}

		// Check if API is available
		if !a.isAPIServiceAvailable(apiService) {
			status.Status = "NotPresent"
			met = false
		} else {
			status.Status = "Present"
			status.UUID = string(apiService.GetUID())
		}
		statuses = append(statuses, status)
	}

	// Check owned API services
	for _, r := range ownedAPIServiceDescs {
		name := fmt.Sprintf("%s.%s", r.Version, r.Group)
		status := v1alpha1.RequirementStatus{
			Group:   "apiregistration.k8s.io",
			Version: "v1",
			Kind:    "APIService",
			Name:    name,
		}

		found := false
		for _, spec := range strategyDetailsDeployment.DeploymentSpecs {
			if spec.Name == r.DeploymentName {
				status.Status = "DeploymentFound"
				statuses = append(statuses, status)
				found = true
				break
			}
		}

		if !found {
			status.Status = "DeploymentNotFound"
			statuses = append(statuses, status)
			met = false
		}
	}

	for _, r := range requiredNativeAPIs {
		name := fmt.Sprintf("%s.%s", r.Version, r.Group)
		status := v1alpha1.RequirementStatus{
			Group:   r.Group,
			Version: r.Version,
			Kind:    r.Kind,
			Name:    name,
		}

		if err := a.isGVKRegistered(r.Group, r.Version, r.Kind); err != nil {
			status.Status = v1alpha1.RequirementStatusReasonNotPresent
			status.Message = "Native API does not exist"
			met = false
			statuses = append(statuses, status)
			continue
		} else {
			status.Status = v1alpha1.RequirementStatusReasonPresent
			status.Message = "Native API exists"
			statuses = append(statuses, status)
			continue
		}
	}

	return
}

// permissionStatus checks whether the given CSV's RBAC requirements are met in its namespace
func (a *Operator) permissionStatus(strategyDetailsDeployment *install.StrategyDetailsDeployment, ruleChecker install.RuleChecker, csvNamespace string) (bool, []v1alpha1.RequirementStatus, error) {
	statusesSet := map[string]v1alpha1.RequirementStatus{}

	checkPermissions := func(permissions []install.StrategyDeploymentPermissions, namespace string) (bool, error) {
		met := true
		for _, perm := range permissions {
			saName := perm.ServiceAccountName
			a.Log.Debugf("perm.ServiceAccountName: %s", saName)

			var status v1alpha1.RequirementStatus
			if stored, ok := statusesSet[saName]; !ok {
				status = v1alpha1.RequirementStatus{
					Group:      "",
					Version:    "v1",
					Kind:       "ServiceAccount",
					Name:       saName,
					Status:     v1alpha1.RequirementStatusReasonPresent,
					Dependents: []v1alpha1.DependentStatus{},
				}
			} else {
				status = stored
			}

			// Ensure the ServiceAccount exists
			sa, err := a.OpClient.GetServiceAccount(csvNamespace, perm.ServiceAccountName)
			if err != nil {
				met = false
				status.Status = v1alpha1.RequirementStatusReasonNotPresent
				status.Message = "Service account does not exist"
				statusesSet[saName] = status
				continue
			}

			// Check if PolicyRules are satisfied
			for _, rule := range perm.Rules {
				dependent := v1alpha1.DependentStatus{
					Group:   "rbac.authorization.k8s.io",
					Kind:    "PolicyRule",
					Version: "v1beta1",
				}

				marshalled, err := json.Marshal(rule)
				if err != nil {
					dependent.Status = v1alpha1.DependentStatusReasonNotSatisfied
					dependent.Message = "rule unmarshallable"
					status.Dependents = append(status.Dependents, dependent)
					continue
				}

				var scope string
				if namespace == metav1.NamespaceAll {
					scope = "cluster"
				} else {
					scope = "namespaced"
				}
				dependent.Message = fmt.Sprintf("%s rule:%s", scope, marshalled)

				satisfied, err := ruleChecker.RuleSatisfied(sa, namespace, rule)
				if err != nil {
					return false, err
				} else if !satisfied {
					met = false
					dependent.Status = v1alpha1.DependentStatusReasonNotSatisfied
					status.Status = v1alpha1.RequirementStatusReasonPresentNotSatisfied
					status.Message = "Policy rule not satisfied for service account"
				} else {
					dependent.Status = v1alpha1.DependentStatusReasonSatisfied
				}

				status.Dependents = append(status.Dependents, dependent)
			}

			statusesSet[saName] = status
		}

		return met, nil
	}

	permMet, err := checkPermissions(strategyDetailsDeployment.Permissions, csvNamespace)
	if err != nil {
		return false, nil, err
	}
	clusterPermMet, err := checkPermissions(strategyDetailsDeployment.ClusterPermissions, metav1.NamespaceAll)
	if err != nil {
		return false, nil, err
	}

	statuses := []v1alpha1.RequirementStatus{}
	for key, status := range statusesSet {
		a.Log.Debugf("appending permission status: %s", key)
		statuses = append(statuses, status)
	}

	return permMet && clusterPermMet, statuses, nil
}

// requirementAndPermissionStatus returns the aggregate requirement and permissions statuses for the given CSV
func (a *Operator) requirementAndPermissionStatus(csv *v1alpha1.ClusterServiceVersion) (bool, []v1alpha1.RequirementStatus, error) {
	// Use a StrategyResolver to unmarshal
	strategyResolver := install.StrategyResolver{}
	strategy, err := strategyResolver.UnmarshalStrategy(csv.Spec.InstallStrategy)
	if err != nil {
		return false, nil, err
	}

	// Assume the strategy is for a deployment
	strategyDetailsDeployment, ok := strategy.(*install.StrategyDetailsDeployment)
	if !ok {
		return false, nil, fmt.Errorf("could not cast install strategy as type %T", strategyDetailsDeployment)
	}

	// Check kubernetes version requirement between CSV and server
	minKubeMet, minKubeStatus := a.minKubeVersionStatus(csv.GetName(), csv.Spec.MinKubeVersion)
	reqMet, reqStatuses := a.requirementStatus(strategyDetailsDeployment, csv.GetAllCRDDescriptions(), csv.GetOwnedAPIServiceDescriptions(), csv.GetRequiredAPIServiceDescriptions(), csv.Spec.NativeAPIs)
	allReqStatuses := append(minKubeStatus, reqStatuses...)

	rbacLister := a.lister.RbacV1()
	roleLister := rbacLister.RoleLister()
	roleBindingLister := rbacLister.RoleBindingLister()
	clusterRoleLister := rbacLister.ClusterRoleLister()
	clusterRoleBindingLister := rbacLister.ClusterRoleBindingLister()

	ruleChecker := install.NewCSVRuleChecker(roleLister, roleBindingLister, clusterRoleLister, clusterRoleBindingLister, csv)
	permMet, permStatuses, err := a.permissionStatus(strategyDetailsDeployment, ruleChecker, csv.GetNamespace())
	if err != nil {
		return false, nil, err
	}

	// Aggregate requirement and permissions statuses
	statuses := append(allReqStatuses, permStatuses...)
	met := minKubeMet && reqMet && permMet
	if !met {
		a.Log.WithField("minKubeMet", minKubeMet).WithField("reqMet", reqMet).WithField("permMet", permMet).Debug("permissions/requirements not met")
	}

	return met, statuses, nil
}

func (a *Operator) isGVKRegistered(group, version, kind string) error {
	logger := a.Log.WithFields(logrus.Fields{
		"group":   group,
		"version": version,
		"kind":    kind,
	})

	gv := metav1.GroupVersion{Group: group, Version: version}
	resources, err := a.OpClient.KubernetesInterface().Discovery().ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		logger.WithField("err", err).Info("could not query for GVK in api discovery")
		return err
	}

	for _, r := range resources.APIResources {
		if r.Kind == kind {
			return nil
		}
	}

	logger.Info("couldn't find GVK in api discovery")
	return olmErrors.GroupVersionKindNotFoundError{group, version, kind}
}
