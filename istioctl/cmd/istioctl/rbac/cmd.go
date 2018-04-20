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

package rbac

import (
	"fmt"

	"github.com/spf13/cobra"
)

type subjectArgs struct {
	user       string
	groups     string
	properties []string
}

type actionArgs struct {
	namespace  string
	service    string
	method     string
	path       string
	properties []string
}

// cani() allows user to query Istio RBAC effect for a specific request.
func cani() *cobra.Command {
	subject := subjectArgs{}
	action := actionArgs{}

	cmd := &cobra.Command{
		Use:   "can-i",
		Short: "Query Istio RBAC policy effect for a specific request",
		Long: `
This command lets you query whether a specific request is allowed or denied under current Istio RBAC
policies. You can construct a fake request with custom subject and target to check if your Istio RBAC
policies is working as expected. Note the fake request is only used locally to evaluate the effect of
the Istio RBAC policies, no actual request will be issued.
`,
		Example: `# Query if user test is allowed to GET pageA of rating service in default namespace.
istioctl experimental rbac can-i --subject-user test --target-method=GET --target-service=rating --target-path=pageA --target-namespace=default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(yangminzhu): Implement the query logic.
			return fmt.Errorf("not implemented.\nsubject: %+v and action: %+v", subject, action)
		},
	}

	cmd.PersistentFlags().StringVar(&subject.user, "subject-user", "",
		"User name/ID that the subject represents.")
	cmd.PersistentFlags().StringVar(&subject.groups, "subject-groups", "",
		"Group name/ID that the subject represents.")
	cmd.PersistentFlags().StringArrayVar(&subject.properties, "subject-properties", []string{},
		"Additional data about the subject. Specified as name1=value1,name2=value2,...")

	cmd.PersistentFlags().StringVar(&action.namespace, "target-namespace", "",
		"Namespace the action is taking place in.")
	cmd.PersistentFlags().StringVar(&action.service, "target-service", "",
		"Service the action is being taken on.")
	cmd.PersistentFlags().StringVar(&action.method, "target-method", "",
		"Http method is being taken.")
	cmd.PersistentFlags().StringVar(&action.path, "target-path", "",
		"Http path within the service.")
	cmd.PersistentFlags().StringArrayVar(&action.properties, "target-properties", []string{},
		"Additional data about the target. Specified as name1=value1,name2=value2,...")
	return cmd
}

// Command provides a command named rbac that allows user to interact with Istio RBAC policies.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rbac",
		Short: "Interact with Istio RBAC policies",
		Long: `
A group of commands used to interact with Istio RBAC policies. For example, Query whether a specific
request is allowed or denied under the current Istio RBAC policies.`,
		Example: `# Query if user test is allowed to GET pageA of rating service in default namespace.
istioctl experimental rbac can-i --subject-user test --target-method=GET --target-service=rating --target-path=pageA --target-namespace=default`,
	}

	cmd.AddCommand(cani())
	return cmd
}
