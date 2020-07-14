// Copyright Istio Authors
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

package mesh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/util"
)

// YAMLSuffix is the suffix of a YAML file.
const YAMLSuffix = ".yaml"

type manifestDiffArgs struct {
	// compareDir indicates comparison between directory.
	compareDir bool
	// verbose generates verbose output.
	verbose bool
	// selectResources constrains the list of resources to compare to only the ones in this list, ignoring all others.
	// The format of each list item is :: and the items are comma separated. The * character represents wildcard selection.
	// e.g.
	// Deployment:istio-system:* - compare all deployments in istio-system namespace
	// Service:*:istio-pilot - compare Services called "istio-pilot" in all namespaces.
	selectResources string
	// ignoreResources ignores all listed items during comparison. It uses the same list format as selectResources.
	ignoreResources string
	// renameResources identifies renamed resources before comparison.
	// The format of each renaming pair is A->B, all renaming pairs are comma separated.
	// e.g. Service:*:istio-pilot->Service:*:istio-control - rename istio-pilot service into istio-control
	renameResources string
}

func addManifestDiffFlags(cmd *cobra.Command, diffArgs *manifestDiffArgs) {
	cmd.PersistentFlags().BoolVarP(&diffArgs.compareDir, "directory", "r",
		false, "Compare directory.")
	cmd.PersistentFlags().BoolVarP(&diffArgs.verbose, "verbose", "v",
		false, "Verbose output.")
	cmd.PersistentFlags().StringVar(&diffArgs.selectResources, "select", "::",
		"Constrain the list of resources to compare to only the ones in this list, ignoring all others.\n"+
			"The format of each list item is \"::\" and the items are comma separated. The \"*\" character represents wildcard selection.\n"+
			"e.g.\n"+
			"    Deployment:istio-system:* - compare all deployments in istio-system namespace\n"+
			"    Service:*:istiod - compare Services called \"istiod\" in all namespaces")
	cmd.PersistentFlags().StringVar(&diffArgs.ignoreResources, "ignore", "",
		"Ignore all listed items during comparison, using the same list format as selectResources.")
	cmd.PersistentFlags().StringVar(&diffArgs.renameResources, "rename", "",
		"Rename resources before comparison.\n"+
			"The format of each renaming pair is A->B, all renaming pairs are comma separated.\n"+
			"e.g. Service:*:istiod->Service:*:istio-control - rename istiod service into istio-control")
}

func manifestDiffCmd(rootArgs *rootArgs, diffArgs *manifestDiffArgs) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <file|dir> <file|dir>",
		Short: "Compare manifests and generate diff",
		Long:  "The diff subcommand compares manifests from two files or directories.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("diff requires two files or directories")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			var equal bool
			if diffArgs.compareDir {
				equal, err = compareManifestsFromDirs(rootArgs, diffArgs.verbose, args[0], args[1],
					diffArgs.renameResources, diffArgs.selectResources, diffArgs.ignoreResources)
				if err != nil {
					return err
				}
				if !equal {
					os.Exit(1)
				}
				return nil
			}

			equal, err = compareManifestsFromFiles(rootArgs, args, diffArgs.verbose,
				diffArgs.renameResources, diffArgs.selectResources, diffArgs.ignoreResources)
			if err != nil {
				return err
			}
			if !equal {
				os.Exit(1)
			}
			return nil
		}}
	return cmd
}

//compareManifestsFromFiles compares two manifest files
func compareManifestsFromFiles(rootArgs *rootArgs, args []string, verbose bool,
	renameResources, selectResources, ignoreResources string) (bool, error) {
	initLogsOrExit(rootArgs)

	a, err := ioutil.ReadFile(args[0])
	if err != nil {
		return false, fmt.Errorf("could not read %q: %v", args[0], err)
	}
	b, err := ioutil.ReadFile(args[1])
	if err != nil {
		return false, fmt.Errorf("could not read %q: %v", args[1], err)
	}

	diff, err := compare.ManifestDiffWithRenameSelectIgnore(string(a), string(b), renameResources, selectResources,
		ignoreResources, verbose)
	if err != nil {
		return false, err
	}
	if diff != "" {
		fmt.Printf("Differences in manifests are:\n%s\n", diff)
		return false, nil
	}

	fmt.Println("Manifests are identical")
	return true, nil
}

func yamlFileFilter(path string) bool {
	return filepath.Ext(path) == YAMLSuffix
}

//compareManifestsFromDirs compares manifests from two directories
func compareManifestsFromDirs(rootArgs *rootArgs, verbose bool, dirName1, dirName2,
	renameResources, selectResources, ignoreResources string) (bool, error) {
	initLogsOrExit(rootArgs)

	mf1, err := util.ReadFilesWithFilter(dirName1, yamlFileFilter)
	if err != nil {
		return false, err
	}
	mf2, err := util.ReadFilesWithFilter(dirName2, yamlFileFilter)
	if err != nil {
		return false, err
	}

	diff, err := compare.ManifestDiffWithRenameSelectIgnore(mf1, mf2, renameResources, selectResources,
		ignoreResources, verbose)
	if err != nil {
		return false, err
	}
	if diff != "" {
		fmt.Printf("Differences in manifests are:\n%s\n", diff)
		return false, nil
	}

	fmt.Println("Manifests are identical")
	return true, nil
}
