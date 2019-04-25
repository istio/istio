/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rbac

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ghodss/yaml"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-tools/pkg/internal/general"
)

// ManifestOptions represent options for generating the RBAC manifests.
type ManifestOptions struct {
	InputDir  string
	OutputDir string
	Name      string
	Labels    map[string]string
}

// SetDefaults sets up the default options for RBAC Manifest generator.
func (o *ManifestOptions) SetDefaults() {
	o.Name = "manager"
	o.InputDir = filepath.Join(".", "pkg")
	o.OutputDir = filepath.Join(".", "config", "rbac")
}

// RoleName returns the RBAC role name to be used in the manifests.
func (o *ManifestOptions) RoleName() string {
	return o.Name + "-role"
}

// RoleBindingName returns the RBAC role binding name to be used in the manifests.
func (o *ManifestOptions) RoleBindingName() string {
	return o.Name + "-rolebinding"
}

// Namespace returns the namespace to be used in the RBAC manifests.
func (o *ManifestOptions) Namespace() string {
	// TODO(droot): define this as a constant and share it with scaffold pkg.
	return "system"
}

// Validate validates the input options.
func (o *ManifestOptions) Validate() error {
	if _, err := os.Stat(o.InputDir); err != nil {
		return fmt.Errorf("invalid input directory '%s' %v", o.InputDir, err)
	}
	return nil
}

// Generate generates RBAC manifests by parsing the RBAC annotations in Go source
// files specified in the input directory.
func Generate(o *ManifestOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	ops := parserOptions{
		rules: []rbacv1.PolicyRule{},
	}
	err := general.ParseDir(o.InputDir, ops.parseAnnotation)
	if err != nil {
		return fmt.Errorf("failed to parse the input dir %v", err)
	}
	if len(ops.rules) == 0 {
		return nil
	}
	roleManifest, err := getClusterRoleManifest(ops.rules, o)
	if err != nil {
		return fmt.Errorf("failed to generate role manifest %v", err)
	}

	roleBindingManifest, err := getClusterRoleBindingManifest(o)
	if err != nil {
		return fmt.Errorf("failed to generate role binding manifests %v", err)
	}

	err = os.MkdirAll(o.OutputDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create output dir %v", err)
	}
	roleManifestFile := filepath.Join(o.OutputDir, "rbac_role.yaml")
	if err := ioutil.WriteFile(roleManifestFile, roleManifest, 0666); err != nil {
		return fmt.Errorf("failed to write role manifest YAML file %v", err)
	}

	roleBindingManifestFile := filepath.Join(o.OutputDir, "rbac_role_binding.yaml")
	if err := ioutil.WriteFile(roleBindingManifestFile, roleBindingManifest, 0666); err != nil {
		return fmt.Errorf("failed to write role manifest YAML file %v", err)
	}
	return nil
}

func getClusterRoleManifest(rules []rbacv1.PolicyRule, o *ManifestOptions) ([]byte, error) {
	role := rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   o.RoleName(),
			Labels: o.Labels,
		},
		Rules: rules,
	}
	return yaml.Marshal(role)
}

func getClusterRoleBindingManifest(o *ManifestOptions) ([]byte, error) {
	rolebinding := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   o.RoleBindingName(),
			Labels: o.Labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Name:      "default",
				Namespace: o.Namespace(),
				Kind:      "ServiceAccount",
			},
		},
		RoleRef: rbacv1.RoleRef{
			Name:     o.RoleName(),
			Kind:     "ClusterRole",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	return yaml.Marshal(rolebinding)
}
