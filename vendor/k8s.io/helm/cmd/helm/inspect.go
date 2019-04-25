/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/spf13/cobra"
	"io"
	"strings"

	"k8s.io/helm/pkg/chartutil"
)

const inspectDesc = `
This command inspects a chart and displays information. It takes a chart reference
('stable/drupal'), a full path to a directory or packaged chart, or a URL.

Inspect prints the contents of the Chart.yaml file and the values.yaml file.
`

const inspectValuesDesc = `
This command inspects a chart (directory, file, or URL) and displays the contents
of the values.yaml file
`

const inspectChartDesc = `
This command inspects a chart (directory, file, or URL) and displays the contents
of the Charts.yaml file
`

const readmeChartDesc = `
This command inspects a chart (directory, file, or URL) and displays the contents
of the README file
`

type inspectCmd struct {
	chartpath string
	output    string
	verify    bool
	keyring   string
	out       io.Writer
	version   string
	repoURL   string
	username  string
	password  string
	devel     bool

	certFile string
	keyFile  string
	caFile   string
}

const (
	chartOnly  = "chart"
	valuesOnly = "values"
	readmeOnly = "readme"
	all        = "all"
)

var readmeFileNames = []string{"readme.md", "readme.txt", "readme"}

func newInspectCmd(out io.Writer) *cobra.Command {
	insp := &inspectCmd{
		out:    out,
		output: all,
	}

	inspectCommand := &cobra.Command{
		Use:   "inspect [CHART]",
		Short: "inspect a chart",
		Long:  inspectDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkArgsLength(len(args), "chart name"); err != nil {
				return err
			}
			if err := insp.prepare(args[0]); err != nil {
				return err
			}
			return insp.run()
		},
	}

	valuesSubCmd := &cobra.Command{
		Use:   "values [CHART]",
		Short: "shows inspect values",
		Long:  inspectValuesDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			insp.output = valuesOnly
			if err := checkArgsLength(len(args), "chart name"); err != nil {
				return err
			}
			if err := insp.prepare(args[0]); err != nil {
				return err
			}
			return insp.run()
		},
	}

	chartSubCmd := &cobra.Command{
		Use:   "chart [CHART]",
		Short: "shows inspect chart",
		Long:  inspectChartDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			insp.output = chartOnly
			if err := checkArgsLength(len(args), "chart name"); err != nil {
				return err
			}
			if err := insp.prepare(args[0]); err != nil {
				return err
			}
			return insp.run()
		},
	}

	readmeSubCmd := &cobra.Command{
		Use:   "readme [CHART]",
		Short: "shows inspect readme",
		Long:  readmeChartDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			insp.output = readmeOnly
			if err := checkArgsLength(len(args), "chart name"); err != nil {
				return err
			}
			if err := insp.prepare(args[0]); err != nil {
				return err
			}
			return insp.run()
		},
	}

	cmds := []*cobra.Command{inspectCommand, readmeSubCmd, valuesSubCmd, chartSubCmd}
	vflag := "verify"
	vdesc := "verify the provenance data for this chart"
	for _, subCmd := range cmds {
		subCmd.Flags().BoolVar(&insp.verify, vflag, false, vdesc)
	}

	kflag := "keyring"
	kdesc := "path to the keyring containing public verification keys"
	kdefault := defaultKeyring()
	for _, subCmd := range cmds {
		subCmd.Flags().StringVar(&insp.keyring, kflag, kdefault, kdesc)
	}

	verflag := "version"
	verdesc := "version of the chart. By default, the newest chart is shown"
	for _, subCmd := range cmds {
		subCmd.Flags().StringVar(&insp.version, verflag, "", verdesc)
	}

	repoURL := "repo"
	repoURLdesc := "chart repository url where to locate the requested chart"
	for _, subCmd := range cmds {
		subCmd.Flags().StringVar(&insp.repoURL, repoURL, "", repoURLdesc)
	}

	username := "username"
	usernamedesc := "chart repository username where to locate the requested chart"
	inspectCommand.Flags().StringVar(&insp.username, username, "", usernamedesc)
	valuesSubCmd.Flags().StringVar(&insp.username, username, "", usernamedesc)
	chartSubCmd.Flags().StringVar(&insp.username, username, "", usernamedesc)

	password := "password"
	passworddesc := "chart repository password where to locate the requested chart"
	inspectCommand.Flags().StringVar(&insp.password, password, "", passworddesc)
	valuesSubCmd.Flags().StringVar(&insp.password, password, "", passworddesc)
	chartSubCmd.Flags().StringVar(&insp.password, password, "", passworddesc)

	develFlag := "devel"
	develDesc := "use development versions, too. Equivalent to version '>0.0.0-0'. If --version is set, this is ignored."
	for _, subCmd := range cmds {
		subCmd.Flags().BoolVar(&insp.devel, develFlag, false, develDesc)
	}

	certFile := "cert-file"
	certFiledesc := "verify certificates of HTTPS-enabled servers using this CA bundle"
	for _, subCmd := range cmds {
		subCmd.Flags().StringVar(&insp.certFile, certFile, "", certFiledesc)
	}

	keyFile := "key-file"
	keyFiledesc := "identify HTTPS client using this SSL key file"
	for _, subCmd := range cmds {
		subCmd.Flags().StringVar(&insp.keyFile, keyFile, "", keyFiledesc)
	}

	caFile := "ca-file"
	caFiledesc := "chart repository url where to locate the requested chart"
	for _, subCmd := range cmds {
		subCmd.Flags().StringVar(&insp.caFile, caFile, "", caFiledesc)
	}

	for _, subCmd := range cmds[1:] {
		inspectCommand.AddCommand(subCmd)
	}

	return inspectCommand
}

func (i *inspectCmd) prepare(chart string) error {
	debug("Original chart version: %q", i.version)
	if i.version == "" && i.devel {
		debug("setting version to >0.0.0-0")
		i.version = ">0.0.0-0"
	}

	cp, err := locateChartPath(i.repoURL, i.username, i.password, chart, i.version, i.verify, i.keyring,
		i.certFile, i.keyFile, i.caFile)
	if err != nil {
		return err
	}
	i.chartpath = cp
	return nil
}

func (i *inspectCmd) run() error {
	chrt, err := chartutil.Load(i.chartpath)
	if err != nil {
		return err
	}
	cf, err := yaml.Marshal(chrt.Metadata)
	if err != nil {
		return err
	}

	if i.output == chartOnly || i.output == all {
		fmt.Fprintln(i.out, string(cf))
	}

	if (i.output == valuesOnly || i.output == all) && chrt.Values != nil {
		if i.output == all {
			fmt.Fprintln(i.out, "---")
		}
		fmt.Fprintln(i.out, chrt.Values.Raw)
	}

	if i.output == readmeOnly || i.output == all {
		if i.output == all {
			fmt.Fprintln(i.out, "---")
		}
		readme := findReadme(chrt.Files)
		if readme == nil {
			return nil
		}
		fmt.Fprintln(i.out, string(readme.Value))
	}
	return nil
}

func findReadme(files []*any.Any) (file *any.Any) {
	for _, file := range files {
		if containsString(readmeFileNames, strings.ToLower(file.TypeUrl), nil) {
			return file
		}
	}
	return nil
}

// containsString checks if a given slice of strings contains the provided string.
// If a modifier func is provided, it is called with the slice item before the comparison.
func containsString(slice []string, s string, modifier func(s string) string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
		if modifier != nil && modifier(item) == s {
			return true
		}
	}
	return false
}
