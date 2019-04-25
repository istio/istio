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
	"os"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newAddCRDCmd - add crd command
func newAddCRDCmd() *cobra.Command {
	crdCmd := &cobra.Command{
		Use:   "crd",
		Short: "Adds a Custom Resource Definition (CRD) and the Custom Resource (CR) files",
		Long: `The operator-sdk add crd command will create a Custom Resource Definition (CRD) and the Custom Resource (CR) files for the specified api-version and kind.

Generated CRD filename: <project-name>/deploy/crds/<group>_<version>_<kind>_crd.yaml
Generated CR  filename: <project-name>/deploy/crds/<group>_<version>_<kind>_cr.yaml

	<project-name>/deploy path must already exist
	--api-version and --kind are required flags to generate the new operator application.
`,
		RunE: crdFunc,
	}
	crdCmd.Flags().StringVar(&apiVersion, "api-version", "", "Kubernetes apiVersion and has a format of $GROUP_NAME/$VERSION (e.g app.example.com/v1alpha1)")
	if err := crdCmd.MarkFlagRequired("api-version"); err != nil {
		log.Fatalf("Failed to mark `api-version` flag for `add crd` subcommand as required")
	}
	crdCmd.Flags().StringVar(&kind, "kind", "", "Kubernetes CustomResourceDefintion kind. (e.g AppService)")
	if err := crdCmd.MarkFlagRequired("kind"); err != nil {
		log.Fatalf("Failed to mark `kind` flag for `add crd` subcommand as required")
	}
	return crdCmd
}

func crdFunc(cmd *cobra.Command, args []string) error {
	cfg := &input.Config{
		AbsProjectPath: projutil.MustGetwd(),
	}
	if len(args) != 0 {
		return fmt.Errorf("command %s doesn't accept any arguments", cmd.CommandPath())
	}
	if err := verifyCRDFlags(); err != nil {
		return err
	}
	if err := verifyCRDDeployPath(); err != nil {
		return err
	}

	log.Infof("Generating Custom Resource Definition (CRD) version %s for kind %s.", apiVersion, kind)

	// generate CR/CRD file
	resource, err := scaffold.NewResource(apiVersion, kind)
	if err != nil {
		return err
	}

	s := scaffold.Scaffold{}
	err = s.Execute(cfg,
		&scaffold.CRD{
			Input:        input.Input{IfExistsAction: input.Skip},
			Resource:     resource,
			IsOperatorGo: projutil.IsOperatorGo(),
		},
		&scaffold.CR{
			Input:    input.Input{IfExistsAction: input.Skip},
			Resource: resource,
		},
	)
	if err != nil {
		return fmt.Errorf("crd scaffold failed: (%v)", err)
	}

	// update deploy/role.yaml for the given resource r.
	if err := scaffold.UpdateRoleForResource(resource, cfg.AbsProjectPath); err != nil {
		return fmt.Errorf("failed to update the RBAC manifest for the resource (%v, %v): (%v)", resource.APIVersion, resource.Kind, err)
	}

	log.Info("CRD generation complete.")
	return nil
}

func verifyCRDFlags() error {
	if len(apiVersion) == 0 {
		return fmt.Errorf("value of --api-version must not have empty value")
	}
	if len(kind) == 0 {
		return fmt.Errorf("value of --kind must not have empty value")
	}
	kindFirstLetter := string(kind[0])
	if kindFirstLetter != strings.ToUpper(kindFirstLetter) {
		return fmt.Errorf("value of --kind must start with an uppercase letter")
	}
	if strings.Count(apiVersion, "/") != 1 {
		return fmt.Errorf("value of --api-version has wrong format (%v); format must be $GROUP_NAME/$VERSION (e.g app.example.com/v1alpha1)", apiVersion)
	}
	return nil
}

// verifyCRDDeployPath checks if the path <project-name>/deploy sub-directory is exists, and that is rooted under $GOPATH
func verifyCRDDeployPath() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to determine the full path of the current directory: (%v)", err)
	}
	// check if the deploy sub-directory exist
	_, err = os.Stat(filepath.Join(wd, scaffold.DeployDir))
	if err != nil {
		return fmt.Errorf("the path (./%v) does not exist. run this command in your project directory", scaffold.DeployDir)
	}
	return nil
}
