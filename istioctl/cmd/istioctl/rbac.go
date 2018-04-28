// Copyright 2018 Istio Authors.
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

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/adapter/rbac"
	"istio.io/istio/mixer/template/authorization"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

type rbacLogger struct{}

// Infof from adapter.Logger.
func (l rbacLogger) Infof(format string, args ...interface{}) {
	// Redirect info to debug for istioctl.
	log.Debugf(format, args...)
}

// Warningf from adapter.Logger.
func (l rbacLogger) Warningf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Errorf from adapter.Logger.
func (l rbacLogger) Errorf(format string, args ...interface{}) error {
	s := fmt.Sprintf(format, args...)
	log.Errorf(s)
	return errors.New(s)
}

// Debugf from adapter.Logger.
func (l rbacLogger) Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// InfoEnabled from adapter.Logger.
func (l rbacLogger) InfoEnabled() bool {
	return false
}

// WarnEnabled from adapter.Logger.
func (l rbacLogger) WarnEnabled() bool {
	return false
}

// ErrorEnabled from adapter.Logger.
func (l rbacLogger) ErrorEnabled() bool {
	return false
}

// DebugEnabled from adapter.Logger.
func (l rbacLogger) DebugEnabled() bool {
	return false
}

// can allows user to query Istio RBAC effect for a specific request.
func can() *cobra.Command {
	instance := &authorization.Instance{
		Subject: &authorization.Subject{},
		Action:  &authorization.Action{},
	}
	subjectProperties := make([]string, 0)
	actionProperties := make([]string, 0)

	cmd := &cobra.Command{
		Use:   "can METHOD SERVICE PATH",
		Short: "Query Istio RBAC policy effect for a specific request",
		Long: `
This command lets you query whether a specific request will be allowed or denied under current Istio
RBAC policies. It constructs a fake request with the custom subject and action specified in the command
line to check if your Istio RBAC policies are working as expected. Note the fake request is only used
locally to evaluate the effect of the Istio RBAC policies, no actual request will be issued.

METHOD is the HTTP method being taken, like GET, POST, etc. SERVICE is the short service name the action
is being taken on. PATH is the HTTP path within the service.`,
		Example: `# Query if user test is allowed to GET /v1/health of service rating.
istioctl experimental rbac can -u test GET rating /v1/health

# Query if service product-page is allowed to POST to /data of service rating with label version=dev.
istioctl experimental rbac can -s service=product-page POST rating /data -a version=dev`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			instance.Action.Method = args[0]
			instance.Action.Service = args[1]
			instance.Action.Path = args[2]
			if namespace == "" {
				instance.Action.Namespace = "default"
			} else {
				instance.Action.Namespace = namespace
			}

			if err := fillInstanceProperties(&instance.Subject.Properties, subjectProperties); err != nil {
				return err
			}
			if err := fillInstanceProperties(&instance.Action.Properties, subjectProperties); err != nil {
				return err
			}

			rbacStore, err := newRbacStore()
			if err != nil {
				return fmt.Errorf("failed to create rbacStore: %v", err)
			}

			ret, err := rbacStore.CheckPermission(instance, rbacLogger{})
			if err != nil {
				return err
			}

			if ret {
				fmt.Println("allow")
			} else {
				fmt.Println("deny")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&instance.Subject.User, "user", "u", "",
		"[Subject] User name/ID that the subject represents.")
	cmd.Flags().StringVarP(&instance.Subject.Groups, "groups", "g", "",
		"[Subject] Group name/ID that the subject represents.")
	cmd.Flags().StringArrayVarP(&subjectProperties, "subject-properties", "s", []string{},
		"[Subject] Additional data about the subject. Specified as name1=value1,name2=value2,...")
	cmd.Flags().StringArrayVarP(&actionProperties, "action-properties", "a", []string{},
		"[Action] Additional data about the action. Specified as name1=value1,name2=value2,...")
	return cmd
}

// Rbac provides a command named rbac that allows user to interact with Istio RBAC policies.
func Rbac() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rbac",
		Short: "Interact with Istio RBAC policies",
		Long: `
A group of commands used to interact with Istio RBAC policies. For example, Query whether a specific
request is allowed or denied under the current Istio RBAC policies.`,
		Example: `# Query if user test is allowed to GET /v1/health of service rating.
istioctl experimental rbac can -u test GET rating /v1/health`,
	}

	cmd.AddCommand(can())
	return cmd
}

func fillInstanceProperties(properties *map[string]interface{}, arguments []string) error {
	for _, arg := range arguments {
		// Use the part before the first = as key and the remaining part as a string value, this is
		// the only supported format of RBAC adapter for now.
		split := strings.SplitN(arg, "=", 2)
		if len(split) != 2 {
			return fmt.Errorf("invalid property %v, the format should be: key=value", arg)
		}
		value, present := (*properties)[split[0]]
		if present {
			return fmt.Errorf("duplicate property %v, previous value %v", arg, value)
		}
		(*properties)[split[0]] = split[1]
	}
	return nil
}

func newRbacStore() (*rbac.ConfigStore, error) {
	client, err := newClient()
	if err != nil {
		return nil, err
	}

	roles, err := client.List(model.ServiceRole.Type, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ServiceRoles for namespace %v: %v", namespace, err)
	}

	bindings, err := client.List(model.ServiceRoleBinding.Type, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ServiceRoleBinding for namespace %v: %v", namespace, err)
	}

	rolesMap := make(rbac.RolesMapByNamespace)
	for _, role := range roles {
		proto := role.Spec.(*rbacproto.ServiceRole)
		err := rolesMap.AddServiceRole(role.Name, role.Namespace, proto)
		if err != nil {
			return nil, err
		}
	}

	for _, binding := range bindings {
		proto := binding.Spec.(*rbacproto.ServiceRoleBinding)
		err := rolesMap.AddServiceRoleBinding(binding.Name, binding.Namespace, proto)
		if err != nil {
			return nil, err
		}
	}
	return &rbac.ConfigStore{Roles: rolesMap}, nil
}
