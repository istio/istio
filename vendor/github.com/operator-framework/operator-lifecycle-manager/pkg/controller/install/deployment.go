package install

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	rbac "k8s.io/api/rbac/v1"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/wrappers"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
)

const (
	InstallStrategyNameDeployment = "deployment"
)

// StrategyDeploymentPermissions describe the rbac rules and service account needed by the install strategy
type StrategyDeploymentPermissions struct {
	ServiceAccountName string            `json:"serviceAccountName"`
	Rules              []rbac.PolicyRule `json:"rules"`
}

// StrategyDeploymentSpec contains the name and spec for the deployment ALM should create
type StrategyDeploymentSpec struct {
	Name string                `json:"name"`
	Spec appsv1.DeploymentSpec `json:"spec"`
}

// StrategyDetailsDeployment represents the parsed details of a Deployment
// InstallStrategy.
type StrategyDetailsDeployment struct {
	DeploymentSpecs    []StrategyDeploymentSpec        `json:"deployments"`
	Permissions        []StrategyDeploymentPermissions `json:"permissions,omitempty"`
	ClusterPermissions []StrategyDeploymentPermissions `json:"clusterPermissions,omitempty"`
}

type StrategyDeploymentInstaller struct {
	strategyClient      wrappers.InstallStrategyDeploymentInterface
	owner               ownerutil.Owner
	previousStrategy    Strategy
	templateAnnotations map[string]string
}

func (d *StrategyDetailsDeployment) GetStrategyName() string {
	return InstallStrategyNameDeployment
}

var _ Strategy = &StrategyDetailsDeployment{}
var _ StrategyInstaller = &StrategyDeploymentInstaller{}

func NewStrategyDeploymentInstaller(strategyClient wrappers.InstallStrategyDeploymentInterface, templateAnnotations map[string]string, owner ownerutil.Owner, previousStrategy Strategy) StrategyInstaller {
	return &StrategyDeploymentInstaller{
		strategyClient:      strategyClient,
		owner:               owner,
		previousStrategy:    previousStrategy,
		templateAnnotations: templateAnnotations,
	}
}

func (i *StrategyDeploymentInstaller) installDeployments(deps []StrategyDeploymentSpec) error {
	for _, d := range deps {
		dep := &appsv1.Deployment{Spec: d.Spec}
		dep.SetName(d.Name)
		dep.SetNamespace(i.owner.GetNamespace())

		// Merge annotations (to avoid losing info from pod template)
		annotations := map[string]string{}
		for k, v := range i.templateAnnotations {
			annotations[k] = v
		}
		for k, v := range dep.Spec.Template.GetAnnotations() {
			annotations[k] = v
		}
		dep.Spec.Template.SetAnnotations(annotations)

		ownerutil.AddNonBlockingOwner(dep, i.owner)
		if dep.Labels == nil {
			dep.SetLabels(map[string]string{})
		}
		dep.Labels["olm.owner"] = i.owner.GetName()
		dep.Labels["olm.owner.namespace"] = i.owner.GetNamespace()
		if _, err := i.strategyClient.CreateOrUpdateDeployment(dep); err != nil {
			return err
		}
	}

	return nil
}

func (i *StrategyDeploymentInstaller) cleanupPrevious(current *StrategyDetailsDeployment, previous *StrategyDetailsDeployment) error {
	previousDeploymentsMap := map[string]struct{}{}
	for _, d := range previous.DeploymentSpecs {
		previousDeploymentsMap[d.Name] = struct{}{}
	}
	for _, d := range current.DeploymentSpecs {
		delete(previousDeploymentsMap, d.Name)
	}
	log.Debugf("preparing to cleanup: %s", previousDeploymentsMap)
	// delete deployments in old strategy but not new
	var err error = nil
	for name := range previousDeploymentsMap {
		err = i.strategyClient.DeleteDeployment(name)
	}
	return err
}

func (i *StrategyDeploymentInstaller) Install(s Strategy) error {
	strategy, ok := s.(*StrategyDetailsDeployment)
	if !ok {
		return fmt.Errorf("attempted to install %s strategy with deployment installer", strategy.GetStrategyName())
	}

	if err := i.installDeployments(strategy.DeploymentSpecs); err != nil {
		return err
	}

	if i.previousStrategy != nil {
		previous, ok := i.previousStrategy.(*StrategyDetailsDeployment)
		if !ok {
			return fmt.Errorf("couldn't parse old install %s strategy with deployment installer", previous.GetStrategyName())
		}
		return i.cleanupPrevious(strategy, previous)
	}
	return nil
}

// CheckInstalled can return nil (installed), or errors
// Errors can indicate: some component missing (keep installing), unable to query (check again later), or unrecoverable (failed in a way we know we can't recover from)
func (i *StrategyDeploymentInstaller) CheckInstalled(s Strategy) (installed bool, err error) {
	strategy, ok := s.(*StrategyDetailsDeployment)
	if !ok {
		return false, StrategyError{Reason: StrategyErrReasonInvalidStrategy, Message: fmt.Sprintf("attempted to check %s strategy with deployment installer", strategy.GetStrategyName())}
	}

	// Check deployments
	if err := i.checkForDeployments(strategy.DeploymentSpecs); err != nil {
		return false, err
	}
	return true, nil
}

func (i *StrategyDeploymentInstaller) checkForDeployments(deploymentSpecs []StrategyDeploymentSpec) error {
	var depNames []string
	for _, dep := range deploymentSpecs {
		depNames = append(depNames, dep.Name)
	}

	existingDeployments, err := i.strategyClient.FindAnyDeploymentsMatchingNames(depNames)
	if err != nil {
		return StrategyError{Reason: StrategyErrReasonComponentMissing, Message: fmt.Sprintf("error querying for %s: %s", depNames, err)}
	}

	// compare deployments to see if any need to be created/updated
	existingMap := map[string]*appsv1.Deployment{}
	for _, d := range existingDeployments {
		existingMap[d.GetName()] = d
	}
	for _, spec := range deploymentSpecs {
		dep, exists := existingMap[spec.Name]
		if !exists {
			log.Debugf("missing deployment with name=%s", spec.Name)
			return StrategyError{Reason: StrategyErrReasonComponentMissing, Message: fmt.Sprintf("missing deployment with name=%s", spec.Name)}
		}
		reason, ready, err := DeploymentStatus(dep)
		if err != nil {
			log.Debugf("deployment %s not ready before timeout: %s", dep.Name, err.Error())
			return StrategyError{Reason: StrategyErrReasonTimeout, Message: fmt.Sprintf("deployment %s not ready before timeout: %s", dep.Name, err.Error())}
		}
		if !ready {
			return StrategyError{Reason: StrategyErrReasonWaiting, Message: fmt.Sprintf("waiting for deployment %s to become ready: %s", dep.Name, reason)}
		}

		// check annotations
		if len(i.templateAnnotations) > 0 && dep.Spec.Template.Annotations == nil {
			return StrategyError{Reason: StrategyErrReasonAnnotationsMissing, Message: fmt.Sprintf("no annotations found on deployment")}
		}
		for key, value := range i.templateAnnotations {
			if dep.Spec.Template.Annotations[key] != value {
				return StrategyError{Reason: StrategyErrReasonAnnotationsMissing, Message: fmt.Sprintf("annotations on deployment don't match. couldn't find %s: %s", key, value)}
			}
		}
	}
	return nil
}
