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

package add

import (
	"fmt"

	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/internal/genutil"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	apiVersion string
	kind       string
	headerFile string
)

func newAddApiCmd() *cobra.Command {
	apiCmd := &cobra.Command{
		Use:   "api",
		Short: "Adds a new api definition under pkg/apis",
		Long: `operator-sdk add api --kind=<kind> --api-version=<group/version> creates the
api definition for a new custom resource under pkg/apis. This command must be
run from the project root directory. If the api already exists at
pkg/apis/<group>/<version> then the command will not overwrite and return an
error.

This command runs Kubernetes deepcopy and OpenAPI V3 generators on tagged
types in all paths under pkg/apis. Go code is generated under
pkg/apis/<group>/<version>/zz_generated.{deepcopy,openapi}.go. CRD's are
generated, or updated if they exist for a particular group + version + kind,
under deploy/crds/<group>_<version>_<kind>_crd.yaml; OpenAPI V3 validation YAML
is generated as a 'validation' object.

Example:
	$ operator-sdk add api --api-version=app.example.com/v1alpha1 --kind=AppService
	$ tree pkg/apis
	pkg/apis/
	├── addtoscheme_app_appservice.go
	├── apis.go
	└── app
		└── v1alpha1
			├── doc.go
			├── register.go
			├── appservice_types.go
			├── zz_generated.deepcopy.go
			├── zz_generated.openapi.go
	$ tree deploy/crds
	├── deploy/crds/app_v1alpha1_appservice_cr.yaml
	├── deploy/crds/app_v1alpha1_appservice_crd.yaml
`,
		RunE: apiRun,
	}

	apiCmd.Flags().StringVar(&apiVersion, "api-version", "", "Kubernetes APIVersion that has a format of $GROUP_NAME/$VERSION (e.g app.example.com/v1alpha1)")
	if err := apiCmd.MarkFlagRequired("api-version"); err != nil {
		log.Fatalf("Failed to mark `api-version` flag for `add api` subcommand as required")
	}
	apiCmd.Flags().StringVar(&kind, "kind", "", "Kubernetes resource Kind name. (e.g AppService)")
	if err := apiCmd.MarkFlagRequired("kind"); err != nil {
		log.Fatalf("Failed to mark `kind` flag for `add api` subcommand as required")
	}
	apiCmd.Flags().StringVar(&headerFile, "header-file", "", "Path to file containing headers for generated files.")

	return apiCmd
}

func apiRun(cmd *cobra.Command, args []string) error {
	projutil.MustInProjectRoot()

	// Only Go projects can add apis.
	if err := projutil.CheckGoProjectCmd(cmd); err != nil {
		return err
	}

	log.Infof("Generating api version %s for kind %s.", apiVersion, kind)

	// Create and validate new resource.
	r, err := scaffold.NewResource(apiVersion, kind)
	if err != nil {
		return err
	}

	absProjectPath := projutil.MustGetwd()

	cfg := &input.Config{
		Repo:           projutil.CheckAndGetProjectGoPkg(),
		AbsProjectPath: absProjectPath,
	}

	s := &scaffold.Scaffold{}
	err = s.Execute(cfg,
		&scaffold.Types{Resource: r},
		&scaffold.AddToScheme{Resource: r},
		&scaffold.Register{Resource: r},
		&scaffold.Doc{Resource: r},
		&scaffold.CR{Resource: r},
		&scaffold.CRD{Resource: r, IsOperatorGo: projutil.IsOperatorGo()},
	)
	if err != nil {
		return fmt.Errorf("api scaffold failed: (%v)", err)
	}

	// update deploy/role.yaml for the given resource r.
	if err := scaffold.UpdateRoleForResource(r, absProjectPath); err != nil {
		return fmt.Errorf("failed to update the RBAC manifest for the resource (%v, %v): (%v)", r.APIVersion, r.Kind, err)
	}

	// Run k8s codegen for deepcopy
	if err := genutil.K8sCodegen(headerFile); err != nil {
		return err
	}

	// Generate a validation spec for the new CRD.
	if err := genutil.OpenAPIGen(headerFile); err != nil {
		return err
	}

	log.Info("API generation complete.")
	return nil
}
