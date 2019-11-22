// Copyright 2019 Istio Authors
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

	"istio.io/operator/pkg/compare"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

// YAMLSuffix is the suffix of a YAML file.
const YAMLSuffix = ".yaml"

type manifestDiffArgs struct {
	// compareDir indicates comparison between directory.
	compareDir bool
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
		false, "compare directory")
	cmd.PersistentFlags().StringVar(&diffArgs.selectResources, "select", "::",
		"selectResources constrains the list of resources to compare to only the ones in this list, ignoring all others.\n"+
			"The format of each list item is \"::\" and the items are comma separated. The \"*\" character represents wildcard selection.\n"+
			"e.g.\n"+
			"    Deployment:istio-system:* - compare all deployments in istio-system namespace\n"+
			"    Service:*:istio-pilot - compare Services called \"istio-pilot\" in all namespaces")
	cmd.PersistentFlags().StringVar(&diffArgs.ignoreResources, "ignore", "",
		"ignoreResources ignores all listed items during comparison. It uses the same list format as selectResources")
	cmd.PersistentFlags().StringVar(&diffArgs.renameResources, "rename", "",
		"renameResources identifies renamed resources before comparison.\n"+
			"The format of each renaming pair is A->B, all renaming pairs are comma separated.\n"+
			"e.g. Service:*:istio-pilot->Service:*:istio-control - rename istio-pilot service into istio-control")
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
		Run: func(cmd *cobra.Command, args []string) {
			l := newLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			if diffArgs.compareDir {
				compareManifestsFromDirs(rootArgs, args[0], args[1], diffArgs.renameResources,
					diffArgs.selectResources, diffArgs.ignoreResources)
			} else {
				compareManifestsFromFiles(rootArgs, args, diffArgs.renameResources,
					diffArgs.selectResources, diffArgs.ignoreResources, l)
			}
		}}
	return cmd
}

//compareManifestsFromFiles compares two manifest files
func compareManifestsFromFiles(rootArgs *rootArgs, args []string,
	renameResources, selectResources, ignoreResources string, l *logger) {
	initLogsOrExit(rootArgs)

	a, err := ioutil.ReadFile(args[0])
	if err != nil {
		l.logAndFatal(fmt.Sprintf("Could not read %q: %v\n", args[0], err.Error()))
	}
	b, err := ioutil.ReadFile(args[1])
	if err != nil {
		l.logAndFatal(fmt.Sprintf("Could not read %q: %v\n", args[1], err.Error()))
	}

	diff, err := compare.ManifestDiffWithRenameSelectIgnore(string(a), string(b), renameResources, selectResources,
		ignoreResources, rootArgs.verbose)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	if diff == "" {
		fmt.Println("Manifests are identical")
	} else {
		fmt.Printf("Differences of manifests are:\n%s", diff)
		os.Exit(1)
	}
}

func yamlFileFilter(path string) bool {
	return filepath.Ext(path) == YAMLSuffix
}

//compareManifestsFromDirs compares manifests from two directories
func compareManifestsFromDirs(rootArgs *rootArgs, dirName1, dirName2,
	renameResources, selectResources, ignoreResources string) {
	initLogsOrExit(rootArgs)

	mf1, err := util.ReadFilesWithFilter(dirName1, yamlFileFilter)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	mf2, err := util.ReadFilesWithFilter(dirName2, yamlFileFilter)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	diff, err := compare.ManifestDiffWithRenameSelectIgnore(mf1, mf2, renameResources, selectResources,
		ignoreResources, rootArgs.verbose)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	if diff == "" {
		fmt.Println("Manifests are identical")
	} else {
		fmt.Printf("Differences of manifests are:\n%s", diff)
		os.Exit(1)
	}
}
