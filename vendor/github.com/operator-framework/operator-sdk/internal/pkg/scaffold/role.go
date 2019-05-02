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

package scaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/fileutil"

	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
	rbacv1 "k8s.io/api/rbac/v1"
	cgoscheme "k8s.io/client-go/kubernetes/scheme"
)

const RoleYamlFile = "role.yaml"

type Role struct {
	input.Input

	IsClusterScoped bool
}

func (s *Role) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = filepath.Join(DeployDir, RoleYamlFile)
	}
	s.TemplateBody = roleTemplate
	return s.Input, nil
}

func UpdateRoleForResource(r *Resource, absProjectPath string) error {
	// append rbac rule to deploy/role.yaml
	roleFilePath := filepath.Join(absProjectPath, DeployDir, RoleYamlFile)
	roleYAML, err := ioutil.ReadFile(roleFilePath)
	if err != nil {
		return fmt.Errorf("failed to read role manifest %v: %v", roleFilePath, err)
	}
	obj, _, err := cgoscheme.Codecs.UniversalDeserializer().Decode(roleYAML, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to decode role manifest %v: %v", roleFilePath, err)
	}
	switch role := obj.(type) {
	// TODO: use rbac/v1.
	case *rbacv1.Role:
		pr := &rbacv1.PolicyRule{}
		apiGroupFound := false
		for i := range role.Rules {
			if role.Rules[i].APIGroups[0] == r.FullGroup {
				apiGroupFound = true
				pr = &role.Rules[i]
				break
			}
		}
		// check if the resource already exists
		for _, resource := range pr.Resources {
			if resource == r.Resource {
				log.Infof("RBAC rules in deploy/role.yaml already up to date for the resource (%v, %v)", r.APIVersion, r.Kind)
				return nil
			}
		}

		pr.Resources = append(pr.Resources, r.Resource)
		// create a new apiGroup if not found.
		if !apiGroupFound {
			pr.APIGroups = []string{r.FullGroup}
			// Using "*" to allow access to the resource and all its subresources e.g "memcacheds" and "memcacheds/finalizers"
			// https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#ownerreferencespermissionenforcement
			pr.Resources = []string{"*"}
			pr.Verbs = []string{"*"}
			role.Rules = append(role.Rules, *pr)
		}
		// update role.yaml
		d, err := json.Marshal(&role)
		if err != nil {
			return fmt.Errorf("failed to marshal role(%+v): %v", role, err)
		}
		m := &map[string]interface{}{}
		if err = yaml.Unmarshal(d, m); err != nil {
			return fmt.Errorf("failed to unmarshal role(%+v): %v", role, err)
		}
		data, err := yaml.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to marshal role(%+v): %v", role, err)
		}
		if err := ioutil.WriteFile(roleFilePath, data, fileutil.DefaultFileMode); err != nil {
			return fmt.Errorf("failed to update %v: %v", roleFilePath, err)
		}
	case *rbacv1.ClusterRole:
		pr := &rbacv1.PolicyRule{}
		apiGroupFound := false
		for i := range role.Rules {
			if role.Rules[i].APIGroups[0] == r.FullGroup {
				apiGroupFound = true
				pr = &role.Rules[i]
				break
			}
		}
		// check if the resource already exists
		for _, resource := range pr.Resources {
			if resource == r.Resource {
				log.Infof("RBAC rules in deploy/role.yaml already up to date for the resource (%v, %v)", r.APIVersion, r.Kind)
				return nil
			}
		}

		pr.Resources = append(pr.Resources, r.Resource)
		// create a new apiGroup if not found.
		if !apiGroupFound {
			pr.APIGroups = []string{r.FullGroup}
			// Using "*" to allow access to the resource and all its subresources e.g "memcacheds" and "memcacheds/finalizers"
			// https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#ownerreferencespermissionenforcement
			pr.Resources = []string{"*"}
			pr.Verbs = []string{"*"}
			role.Rules = append(role.Rules, *pr)
		}
		// update role.yaml
		d, err := json.Marshal(&role)
		if err != nil {
			return fmt.Errorf("failed to marshal role(%+v): %v", role, err)
		}
		m := &map[string]interface{}{}
		err = yaml.Unmarshal(d, m)
		data, err := yaml.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to marshal role(%+v): %v", role, err)
		}
		if err := ioutil.WriteFile(roleFilePath, data, fileutil.DefaultFileMode); err != nil {
			return fmt.Errorf("failed to update %v: %v", roleFilePath, err)
		}
	default:
		return errors.New("failed to parse role.yaml as a role")
	}
	// not reachable
	return nil
}

const roleTemplate = `kind: {{if .IsClusterScoped}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: {{.ProjectName}}
rules:
- apiGroups:
  - ""
  resources:
  - pods
  - services
  - endpoints
  - persistentvolumeclaims
  - events
  - configmaps
  - secrets
  verbs:
  - "*"
- apiGroups:
  - ""
  resources:
  - namespaces
  verbs:
  - get
- apiGroups:
  - apps
  resources:
  - deployments
  - daemonsets
  - replicasets
  - statefulsets
  verbs:
  - "*"
- apiGroups:
  - monitoring.coreos.com
  resources:
  - servicemonitors
  verbs:
  - "get"
  - "create"
- apiGroups:
  - apps
  resources:
  - deployments/finalizers
  resourceNames:
  - {{ .ProjectName }}
  verbs:
  - "update"
`
