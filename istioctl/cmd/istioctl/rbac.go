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
	"fmt"
	"io"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/adapter/rbac"
	"istio.io/istio/pilot/pkg/model"
)

// list provides a command named list that allows user to query information under current
// Istio RBAC policies.
func list() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Query information under current Istio RBAC policies.",
		Long: `
This command lets you query information under current Istio RBAC policies.
You can list permissions a subject has, or members that are allowed to access a service`,
	}

	cmd.AddCommand(permissions())
	cmd.AddCommand(members())

	return cmd
}

// permissions allows user to list permission under current Istio RBAC policies for a given subject.
func permissions() *cobra.Command {
	subject := rbac.SubjectArgs{}
	action := rbac.ActionArgs{}

	cmd := &cobra.Command{
		Use:   "permissions",
		Short: "List permissions a subject has under current Istio RBAC policies",
		// TODO (jaebong) need to mention about group subject after the official announcement
		Long: `
This command lets you list permissions a subject has under current Istio RBAC policies.

Subject can be either a user/ID or subject properties`,
		Example: `# Query permissions for user test.
istioctl experimental rbac list permissions -u test`,
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {

			if namespace == "" {
				action.Namespace = defaultNamespace
			} else {
				action.Namespace = namespace
			}

			rbacStore, err := newRbacStore()
			if err != nil {
				return fmt.Errorf("failed to create rbacStore: %v", err)
			}

			ret, err := rbacStore.ListPermissions(subject, action)
			if err != nil {
				return fmt.Errorf("failed to get permission list: %v", err)
			}

			var outputters = map[string]func(io.Writer, *map[string]*rbacproto.ServiceRole){
				"yaml":  printAccessRulesYamlOutput,
				"short": printAccessRulesShortOutput,
			}

			if outputFunc, ok := outputters[outputFormat]; ok {
				outputFunc(cmd.OutOrStdout(), ret)
			} else {
				return fmt.Errorf("unknown output format %v. Types are yaml|short", outputFormat)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&subject.User, "user", "u", "",
		"[Subject] User name/ID that the subject represents.")
	cmd.Flags().StringVarP(&subject.Groups, "groups", "g", "",
		"[Subject] Group name/ID that the subject represents.")
	cmd.Flags().StringArrayVarP(&subject.Properties, "subject-properties", "s", []string{},
		"[Subject] Additional data about the subject. Specified as name1=value1,name2=value2,...")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "short",
		"Output format. One of:yaml|short")
	return cmd
}

// members allows user to list members allowed to access the given METHOD, SERVICE, and PATH
func members() *cobra.Command {
	subject := rbac.SubjectArgs{}
	action := rbac.ActionArgs{}

	cmd := &cobra.Command{
		Use:   "members METHOD SERVICE PATH",
		Short: "List members allowed to access a service under current Istio RBAC policies",
		Long: `
This command lets you list members allowed to access a service METHOD, SERVICE, and PATH.

METHOD is the HTTP method being taken, like GET, POST, etc. SERVICE is the short service name the action
is being taken on. PATH is the HTTP path within the service.`,
		Example: `# Query list of members allowed to GET /v1/health of service rating.
istioctl experimental rbac list members GET rating /v1/health -o yaml`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			action.Method = args[0]
			action.Service = args[1]
			action.Path = args[2]
			if namespace == "" {
				action.Namespace = defaultNamespace
			} else {
				action.Namespace = namespace
			}

			rbacStore, err := newRbacStore()
			if err != nil {
				return fmt.Errorf("failed to create rbacStore: %v", err)
			}

			ret, err := rbacStore.ListMembers(subject, action)
			if err != nil {
				return err
			}

			var outputters = map[string]func(io.Writer, *[]string){
				"yaml":  printStringArrayYamlOutput,
				"short": printStringArrayShortOutput,
			}

			if outputFunc, ok := outputters[outputFormat]; ok {
				outputFunc(cmd.OutOrStdout(), ret)
			} else {
				return fmt.Errorf("unknown output format %v. Types are yaml|short", outputFormat)
			}

			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&action.Properties, "action-properties", "a", []string{},
		"[Action] Additional data about the action. Specified as name1=value1,name2=value2,...")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "short",
		"Output format. One of:yaml|short")
	return cmd
}

// can allows user to query Istio RBAC effect for a specific request.
func can() *cobra.Command {
	subject := rbac.SubjectArgs{}
	action := rbac.ActionArgs{}

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
			action.Method = args[0]
			action.Service = args[1]
			action.Path = args[2]
			if namespace == "" {
				action.Namespace = defaultNamespace
			} else {
				action.Namespace = namespace
			}

			rbacStore, err := newRbacStore()
			if err != nil {
				return fmt.Errorf("failed to create rbacStore: %v", err)
			}

			ret, err := rbacStore.Check(subject, action)
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

	cmd.Flags().StringVarP(&subject.User, "user", "u", "",
		"[Subject] User name/ID that the subject represents.")
	cmd.Flags().StringVarP(&subject.Groups, "groups", "g", "",
		"[Subject] Group name/ID that the subject represents.")
	cmd.Flags().StringArrayVarP(&subject.Properties, "subject-properties", "s", []string{},
		"[Subject] Additional data about the subject. Specified as name1=value1,name2=value2,...")
	cmd.Flags().StringArrayVarP(&action.Properties, "action-properties", "a", []string{},
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
	cmd.AddCommand(list())

	return cmd
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

// constraintToString converts AccessRule_Constraint to string
func constraintToString(constraints []*rbacproto.AccessRule_Constraint) string {
	result := make([]string, 0)
	for _, constraint := range constraints {
		result = append(result, constraint.Key+": "+strings.Join(constraint.Values, ", "))
	}
	return strings.Join(result, "\n")
}

// printAccessRuleShortOutput prints access rules in short text format
func printAccessRulesShortOutput(writer io.Writer, serviceRole *map[string]*rbacproto.ServiceRole) {
	table := tablewriter.NewWriter(writer)

	table.SetHeader([]string{"Services", "Methods", "Paths", "Constraints"})
	table.SetBorder(false)
	table.SetCenterSeparator(" ")
	table.SetColumnSeparator(" ")
	table.SetRowLine(false)

	for _, role := range *serviceRole {
		for _, rule := range role.Rules {
			table.Append(
				[]string{strings.Join(rule.Services, "\n"),
					strings.Join(rule.Methods, "\n"),
					strings.Join(rule.Paths, "\n"), constraintToString(rule.Constraints)})
		}
	}

	table.Render()
}

// printAccessRuleYamlOutput prints access rules in Yaml format
func printAccessRulesYamlOutput(writer io.Writer, accessRule *map[string]*rbacproto.ServiceRole) {
	bytes, err := yaml.Marshal(accessRule)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Fprint(writer, string(bytes))
}

// printAccessRuleShortOutput prints access rules in short text format
func printStringArrayShortOutput(writer io.Writer, list *[]string) {
	for _, item := range *list {
		fmt.Fprintf(writer, "%v\n", item)
	}
}

// printAccessRuleYamlOutput print access rules in Yaml format
func printStringArrayYamlOutput(writer io.Writer, list *[]string) {
	bytes, err := yaml.Marshal(list)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Fprint(writer, string(bytes))
}
