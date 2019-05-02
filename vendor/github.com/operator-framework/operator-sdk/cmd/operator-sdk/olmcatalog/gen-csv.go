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

package olmcatalog

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	catalog "github.com/operator-framework/operator-sdk/internal/pkg/scaffold/olm-catalog"
	"github.com/operator-framework/operator-sdk/internal/util/fileutil"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	"github.com/coreos/go-semver/semver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	csvVersion    string
	fromVersion   string
	csvConfigPath string
	updateCRDs    bool
)

func newGenCSVCmd() *cobra.Command {
	genCSVCmd := &cobra.Command{
		Use:   "gen-csv",
		Short: "Generates a Cluster Service Version yaml file for the operator",
		Long: `The gen-csv command generates a Cluster Service Version (CSV) YAML manifest
for the operator. This file is used to publish the operator to the OLM Catalog.

A CSV semantic version is supplied via the --csv-version flag. If your operator
has already generated a CSV manifest you want to use as a base, supply its
version to --from-version. Otherwise the SDK will scaffold a new CSV manifest.

Configure CSV generation by writing a config file 'deploy/olm-catalog/csv-config.yaml`,
		RunE: genCSVFunc,
	}

	genCSVCmd.Flags().StringVar(&csvVersion, "csv-version", "", "Semantic version of the CSV")
	genCSVCmd.MarkFlagRequired("csv-version")
	genCSVCmd.Flags().StringVar(&fromVersion, "from-version", "", "Semantic version of an existing CSV to use as a base")
	genCSVCmd.Flags().StringVar(&csvConfigPath, "csv-config", "", "Path to CSV config file. Defaults to deploy/olm-catalog/csv-config.yaml")
	genCSVCmd.Flags().BoolVar(&updateCRDs, "update-crds", false, "Update CRD manifests in deploy/{operator-name}/{csv-version} the using latest API's")

	return genCSVCmd
}

func genCSVFunc(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command %s doesn't accept any arguments", cmd.CommandPath())
	}

	if err := verifyGenCSVFlags(); err != nil {
		return err
	}

	absProjectPath := projutil.MustGetwd()
	cfg := &input.Config{
		AbsProjectPath: absProjectPath,
		ProjectName:    filepath.Base(absProjectPath),
	}
	if projutil.IsOperatorGo() {
		cfg.Repo = projutil.CheckAndGetProjectGoPkg()
	}

	log.Infof("Generating CSV manifest version %s", csvVersion)

	s := &scaffold.Scaffold{}
	csv := &catalog.CSV{
		CSVVersion:     csvVersion,
		FromVersion:    fromVersion,
		ConfigFilePath: csvConfigPath,
	}
	if err := s.Execute(cfg, csv); err != nil {
		return fmt.Errorf("catalog scaffold failed: (%v)", err)
	}

	// Write CRD's to the new or updated CSV package dir.
	if updateCRDs {
		input, err := csv.GetInput()
		if err != nil {
			return err
		}
		cfg, err := catalog.GetCSVConfig(csvConfigPath)
		if err != nil {
			return err
		}
		err = writeCRDsToDir(cfg.CRDCRPaths, filepath.Dir(input.Path))
		if err != nil {
			return err
		}
	}

	return nil
}

func verifyGenCSVFlags() error {
	if err := verifyCSVVersion(csvVersion); err != nil {
		return err
	}
	if fromVersion != "" {
		if err := verifyCSVVersion(fromVersion); err != nil {
			return err
		}
	}
	if fromVersion != "" && csvVersion == fromVersion {
		return fmt.Errorf("from-version (%s) cannot equal csv-version; set only csv-version instead", fromVersion)
	}
	return nil
}

func verifyCSVVersion(version string) error {
	v, err := semver.NewVersion(version)
	if err != nil {
		return fmt.Errorf("%s is not a valid semantic version: (%v)", version, err)
	}
	// Ensures numerical values composing csvVersion don't contain leading 0's,
	// ex. 01.01.01
	if v.String() != version {
		return fmt.Errorf("provided CSV version %s contains bad values (parses to %s)", version, v)
	}
	return nil
}

func writeCRDsToDir(crdPaths []string, toDir string) error {
	for _, p := range crdPaths {
		if !strings.HasSuffix(p, "crd.yaml") {
			continue
		}
		b, err := ioutil.ReadFile(p)
		if err != nil {
			return err
		}
		path := filepath.Join(toDir, filepath.Base(p))
		err = ioutil.WriteFile(path, b, fileutil.DefaultFileMode)
		if err != nil {
			return err
		}
	}
	return nil
}
