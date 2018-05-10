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

// can allows user to query Istio RBAC effect for a specific request.
func can() *cobra.Command {
	subject := subjectArgs{}
	action := actionArgs{}

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
			action.method = args[0]
			action.service = args[1]
			action.path = args[2]
			action.namespace = namespace

			// TODO(yangminzhu): Implement the query logic.
			return fmt.Errorf("not implemented.\nsubject: %+v and action: %+v", subject, action)
		},
	}

	cmd.Flags().StringVarP(&subject.user, "user", "u", "",
		"[Subject] User name/ID that the subject represents.")
	cmd.Flags().StringVarP(&subject.groups, "groups", "g", "",
		"[Subject] Group name/ID that the subject represents.")
	cmd.Flags().StringArrayVarP(&subject.properties, "subject-properties", "s", []string{},
		"[Subject] Additional data about the subject. Specified as name1=value1,name2=value2,...")
	cmd.Flags().StringArrayVarP(&action.properties, "action-properties", "a", []string{},
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
