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
	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util/clog"
	buildversion "istio.io/pkg/version"
)

type operatorInitArgs struct {
	// inFilenames is the path to the input IstioOperator CR.
	inFilename string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config.
	context string

	// common is shared operator args
	common operatorCommonArgs
}

func addOperatorInitFlags(cmd *cobra.Command, args *operatorInitArgs) {
	hub, tag := buildversion.DockerInfo.Hub, buildversion.DockerInfo.Tag
	if hub == "" {
		hub = "gcr.io/istio-testing"
	}
	if tag == "" {
		tag = "latest"
	}
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", "Path to file containing IstioOperator custom resource")
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use")
	cmd.PersistentFlags().StringVar(&args.common.hub, "hub", hub, "The hub for the operator controller image")
	cmd.PersistentFlags().StringVar(&args.common.tag, "tag", tag, "The tag for the operator controller image")
	cmd.PersistentFlags().StringVar(&args.common.operatorNamespace, "operatorNamespace", operatorDefaultNamespace,
		"The namespace the operator controller is installed into")
	cmd.PersistentFlags().StringVar(&args.common.istioNamespace, "istioNamespace", istioDefaultNamespace,
		"The namespace Istio is installed into. Deprecated, use '--watchedNamespaces' instead.")
	cmd.PersistentFlags().StringVar(&args.common.watchedNamespaces, "watchedNamespaces", istioDefaultNamespace,
		"The namespaces the operator controller watches, could be namespace list separated by comma, eg. 'ns1,ns2'")
	cmd.PersistentFlags().StringVarP(&args.common.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.common.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.common.revision, "revision", "r", "",
		revisionFlagHelpStr)
}

func operatorInitCmd(rootArgs *rootArgs, oiArgs *operatorInitArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Installs the Istio operator controller in the cluster.",
		Long:  "The init subcommand installs the Istio operator controller in the cluster.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			operatorInit(rootArgs, oiArgs, l)
		}}
}

// operatorInit installs the Istio operator controller into the cluster.
func operatorInit(args *rootArgs, oiArgs *operatorInitArgs, l clog.Logger) {
	initLogsOrExit(args)

	restConfig, clientset, client, err := K8sConfig(oiArgs.kubeConfigPath, oiArgs.context)
	if err != nil {
		l.LogAndFatal(err)
	}
	// Error here likely indicates Deployment is missing. If some other K8s error, we will hit it again later.
	already, _ := isControllerInstalled(clientset, oiArgs.common.operatorNamespace, oiArgs.common.revision)
	if already {
		l.LogAndPrintf("Operator controller is already installed in %s namespace, updating.", oiArgs.common.operatorNamespace)
	}

	l.LogAndPrintf("Using operator Deployment image: %s/operator:%s", oiArgs.common.hub, oiArgs.common.tag)

	vals, mstr, err := renderOperatorManifest(args, &oiArgs.common)
	if err != nil {
		l.LogAndFatal(err)
	}

	installerScope.Debugf("Installing operator charts with the following values:\n%s", vals)
	installerScope.Debugf("Using the following manifest to install operator:\n%s\n", mstr)

	opts := &applyOptions{
		DryRun:     args.dryRun,
		Kubeconfig: oiArgs.kubeConfigPath,
		Context:    oiArgs.context,
	}

	// If CR was passed, we must create a namespace for it and install CR into it.
	customResource, istioNamespace, err := getCRAndNamespaceFromFile(oiArgs.inFilename, l)
	if err != nil {
		l.LogAndFatal(err)
	}
	var iop *iopv1alpha1.IstioOperator
	if oiArgs.common.revision != "" {
		emptyiops := &v1alpha1.IstioOperatorSpec{Profile: "empty", Revision: oiArgs.common.revision}
		iop, err = translate.IOPStoIOP(emptyiops, "", "")
		if err != nil {
			l.LogAndFatal(err)
		}
	}

	if err := applyManifest(restConfig, client, mstr, name.IstioOperatorComponentName, opts, iop, l); err != nil {
		l.LogAndFatal(err)
	}

	if customResource != "" {
		if err := createNamespace(clientset, istioNamespace); err != nil {
			l.LogAndFatal(err)

		}
		if err := applyManifest(restConfig, client, customResource, name.IstioOperatorComponentName, opts, iop, l); err != nil {
			l.LogAndFatal(err)
		}
	}

	l.LogAndPrint(color.New(color.FgGreen).Sprint("✔ ") + installationCompleteStr)
}
