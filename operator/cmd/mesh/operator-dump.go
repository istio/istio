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
	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/util"
	buildversion "istio.io/pkg/version"
)

type operatorDumpArgs struct {
	// hub is the hub for the operator image.
	hub string
	// tag is the tag for the operator image.
	tag string
	// operatorNamespace is the namespace the operator controller is installed into.
	operatorNamespace string
	// istioNamespace is the namespace Istio is installed into.
	istioNamespace string
	// inFilenames is the path to the input IstioOperator CR.
	inFilename string
	// outputFormat controls the format of profile dumps
	outputFormat string
}

func addOperatorDumpFlags(cmd *cobra.Command, args *operatorDumpArgs) {
	hub, tag := buildversion.DockerInfo.Hub, buildversion.DockerInfo.Tag
	if hub == "" {
		hub = "gcr.io/istio-testing"
	}
	if tag == "" {
		tag = "latest"
	}
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", "Path to file containing IstioOperator custom resource")
	cmd.PersistentFlags().StringVar(&args.hub, "hub", hub, "The hub for the operator controller image")
	cmd.PersistentFlags().StringVar(&args.tag, "tag", tag, "The tag for the operator controller image")
	cmd.PersistentFlags().StringVar(&args.operatorNamespace, "operatorNamespace", "istio-operator",
		"The namespace the operator controller is installed into")
	cmd.PersistentFlags().StringVar(&args.istioNamespace, "istioNamespace", "istio-system",
		"The namespace Istio is installed into")
	cmd.PersistentFlags().StringVarP(&args.outputFormat, "output", "o", yamlOutput,
		"Output format: one of json|yaml")
}

func operatorDumpCmd(rootArgs *rootArgs, odArgs *operatorDumpArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Dump the Istio operator controller installation manifests.",
		Long:  "The dump subcommand generate the Istio operator controller installation manifests and outputs to the console by default.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			operatorDump(rootArgs, odArgs, l)
		}}
}

// operatorDump generate the Istio operator controller installation manifests.
func operatorDump(args *rootArgs, odArgs *operatorDumpArgs, l *Logger) {
	initLogsOrExit(args)

	y, err := renderOperatorManifest(args, odArgs, l)
	if err != nil {
		l.logAndFatal(err)
	}

	switch odArgs.outputFormat {
	case jsonOutput:
		j, err := yamlToPrettyJSON(y)
		if err != nil {
			l.logAndFatal(err)
		}
		l.print(j + "\n")
	case yamlOutput:
		l.print(y + "\n")
	}
}

// chartsRootDir, helmBaseDir, componentName, namespace string) (TemplateRenderer, error)
func renderOperatorManifest(_ *rootArgs, odArgs *operatorDumpArgs, _ *Logger) (string, error) {
	r, err := helm.NewHelmRenderer("", "../operator-chart", istioControllerComponentName, odArgs.operatorNamespace)
	if err != nil {
		return "", err
	}

	if err := r.Run(); err != nil {
		return "", err
	}

	tmpl := `
operatorNamespace: {{.OperatorNamespace}}
istioNamespace: {{.IstioNamespace}}
hub: {{.Hub}}
tag: {{.Tag}}
`

	tv := struct {
		OperatorNamespace string
		IstioNamespace    string
		Hub               string
		Tag               string
	}{
		OperatorNamespace: odArgs.operatorNamespace,
		IstioNamespace:    odArgs.istioNamespace,
		Hub:               odArgs.hub,
		Tag:               odArgs.tag,
	}
	vals, err := util.RenderTemplate(tmpl, tv)
	if err != nil {
		return "", err
	}
	scope.Infof("Installing operator charts with the following values:\n%s", vals)
	return r.RenderManifest(vals)
}
