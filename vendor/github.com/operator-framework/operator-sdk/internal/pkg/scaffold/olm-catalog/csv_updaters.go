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

package catalog

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	olmapiv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	olminstall "github.com/operator-framework/operator-lifecycle-manager/pkg/controller/install"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

// CSVUpdater is an interface for any data that can be in a CSV, which will be
// set to the corresponding field on Apply().
type CSVUpdater interface {
	// Apply applies a data update to a CSV argument.
	Apply(*olmapiv1alpha1.ClusterServiceVersion) error
}

type updaterStore struct {
	installStrategy *InstallStrategyUpdate
	crds            *CustomResourceDefinitionsUpdate
	almExamples     *ALMExamplesUpdate
}

func NewUpdaterStore() *updaterStore {
	return &updaterStore{
		installStrategy: &InstallStrategyUpdate{
			&olminstall.StrategyDetailsDeployment{},
		},
		crds: &CustomResourceDefinitionsUpdate{
			&olmapiv1alpha1.CustomResourceDefinitions{},
			make(map[string]struct{}),
		},
		almExamples: &ALMExamplesUpdate{},
	}
}

// Apply iteratively calls each stored CSVUpdater's Apply() method.
func (s *updaterStore) Apply(csv *olmapiv1alpha1.ClusterServiceVersion) error {
	updaters := []CSVUpdater{s.installStrategy, s.crds, s.almExamples}
	for _, updater := range updaters {
		if err := updater.Apply(csv); err != nil {
			return err
		}
	}
	return nil
}

func getKindfromYAML(yamlData []byte) (string, error) {
	var temp struct {
		Kind string
	}
	if err := yaml.Unmarshal(yamlData, &temp); err != nil {
		return "", err
	}
	return temp.Kind, nil
}

func (s *updaterStore) AddToUpdater(yamlSpec []byte, kind string) (found bool, err error) {
	found = true
	switch kind {
	case "Role":
		err = s.AddRole(yamlSpec)
	case "ClusterRole":
		err = s.AddClusterRole(yamlSpec)
	case "Deployment":
		err = s.AddDeploymentSpec(yamlSpec)
	case "CustomResourceDefinition":
		// All CRD's present will be 'owned'.
		err = s.AddOwnedCRD(yamlSpec)
	default:
		found = false
	}
	return found, err
}

type InstallStrategyUpdate struct {
	*olminstall.StrategyDetailsDeployment
}

func (store *updaterStore) AddRole(yamlDoc []byte) error {
	role := &rbacv1.Role{}
	if err := yaml.Unmarshal(yamlDoc, role); err != nil {
		return err
	}
	perm := olminstall.StrategyDeploymentPermissions{
		ServiceAccountName: role.ObjectMeta.Name,
		Rules:              role.Rules,
	}
	store.installStrategy.Permissions = append(store.installStrategy.Permissions, perm)

	return nil
}

func (store *updaterStore) AddClusterRole(yamlDoc []byte) error {
	clusterRole := &rbacv1.ClusterRole{}
	if err := yaml.Unmarshal(yamlDoc, clusterRole); err != nil {
		return err
	}
	perm := olminstall.StrategyDeploymentPermissions{
		ServiceAccountName: clusterRole.ObjectMeta.Name,
		Rules:              clusterRole.Rules,
	}
	store.installStrategy.ClusterPermissions = append(store.installStrategy.ClusterPermissions, perm)

	return nil
}

func (store *updaterStore) AddDeploymentSpec(yamlDoc []byte) error {
	dep := &appsv1.Deployment{}
	if err := yaml.Unmarshal(yamlDoc, dep); err != nil {
		return err
	}
	depSpec := olminstall.StrategyDeploymentSpec{
		Name: dep.ObjectMeta.Name,
		Spec: dep.Spec,
	}
	store.installStrategy.DeploymentSpecs = append(store.installStrategy.DeploymentSpecs, depSpec)

	return nil
}

func (u *InstallStrategyUpdate) Apply(csv *olmapiv1alpha1.ClusterServiceVersion) (err error) {
	// Get install strategy from csv. Default to a deployment strategy if none found.
	var strat olminstall.Strategy
	if csv.Spec.InstallStrategy.StrategyName == "" {
		csv.Spec.InstallStrategy.StrategyName = olminstall.InstallStrategyNameDeployment
		strat = &olminstall.StrategyDetailsDeployment{}
	} else {
		var resolver *olminstall.StrategyResolver
		strat, err = resolver.UnmarshalStrategy(csv.Spec.InstallStrategy)
		if err != nil {
			return err
		}
	}

	switch s := strat.(type) {
	case *olminstall.StrategyDetailsDeployment:
		// Update permissions and deployments.
		u.updatePermissions(s)
		u.updateClusterPermissions(s)
		u.updateDeploymentSpecs(s)
	default:
		return fmt.Errorf("install strategy (%v) of unknown type", strat)
	}

	// Re-serialize permissions into csv strategy.
	updatedStrat, err := json.Marshal(strat)
	if err != nil {
		return err
	}
	csv.Spec.InstallStrategy.StrategySpecRaw = updatedStrat

	return nil
}

func (u *InstallStrategyUpdate) updatePermissions(strat *olminstall.StrategyDetailsDeployment) {
	if len(u.Permissions) != 0 {
		strat.Permissions = u.Permissions
	}
}

func (u *InstallStrategyUpdate) updateClusterPermissions(strat *olminstall.StrategyDetailsDeployment) {
	if len(u.ClusterPermissions) != 0 {
		strat.ClusterPermissions = u.ClusterPermissions
	}
}

func (u *InstallStrategyUpdate) updateDeploymentSpecs(strat *olminstall.StrategyDetailsDeployment) {
	if len(u.DeploymentSpecs) != 0 {
		strat.DeploymentSpecs = u.DeploymentSpecs
	}
}

type CustomResourceDefinitionsUpdate struct {
	*olmapiv1alpha1.CustomResourceDefinitions
	crKinds map[string]struct{}
}

func (store *updaterStore) AddOwnedCRD(yamlDoc []byte) error {
	crd := &apiextv1beta1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(yamlDoc, crd); err != nil {
		return err
	}
	store.crds.Owned = append(store.crds.Owned, olmapiv1alpha1.CRDDescription{
		Name:    crd.ObjectMeta.Name,
		Version: crd.Spec.Version,
		Kind:    crd.Spec.Names.Kind,
	})
	store.crds.crKinds[crd.Spec.Names.Kind] = struct{}{}
	return nil
}

// Apply updates csv's "owned" CRDDescriptions. "required" CRDDescriptions are
// left as-is, since they are user-defined values.
func (u *CustomResourceDefinitionsUpdate) Apply(csv *olmapiv1alpha1.ClusterServiceVersion) error {
	set := make(map[string]olmapiv1alpha1.CRDDescription)
	for _, csvDesc := range csv.Spec.CustomResourceDefinitions.Owned {
		set[csvDesc.Name] = csvDesc
	}
	du := u.DeepCopy()
	for i, uDesc := range u.Owned {
		if csvDesc, ok := set[uDesc.Name]; ok {
			csvDesc.Name = uDesc.Name
			csvDesc.Version = uDesc.Version
			csvDesc.Kind = uDesc.Kind
			du.Owned[i] = csvDesc
		}
	}
	csv.Spec.CustomResourceDefinitions.Owned = du.Owned
	return nil
}

type ALMExamplesUpdate struct {
	crs []string
}

func (store *updaterStore) AddCR(yamlDoc []byte) error {
	if len(yamlDoc) == 0 {
		return nil
	}
	crBytes, err := yaml.YAMLToJSON(yamlDoc)
	if err != nil {
		return err
	}
	store.almExamples.crs = append(store.almExamples.crs, string(crBytes))
	return nil
}

func (u *ALMExamplesUpdate) Apply(csv *olmapiv1alpha1.ClusterServiceVersion) error {
	if len(u.crs) == 0 {
		return nil
	}
	if csv.GetAnnotations() == nil {
		csv.SetAnnotations(make(map[string]string))
	}
	sb := &strings.Builder{}
	sb.WriteString(`[`)
	for i, example := range u.crs {
		sb.WriteString(example)
		if i < len(u.crs)-1 {
			sb.WriteString(`,`)
		}
	}
	sb.WriteString(`]`)

	csv.GetAnnotations()["alm-examples"] = sb.String()
	return nil
}
