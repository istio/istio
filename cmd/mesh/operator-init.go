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
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/utils/pointer"

	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/kubectlcmd"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/object"
	"istio.io/operator/pkg/util"
	"istio.io/operator/version"
	"istio.io/pkg/log"
	buildversion "istio.io/pkg/version"
)

type operatorInitArgs struct {
	// hub is the hub for the operator image.
	hub string
	// tag is the tag for the operator image.
	tag string
	// operatorNamespace is the namespace the operator controller is installed into.
	operatorNamespace string
	// istioNamespace is the namespace Istio is installed into.
	istioNamespace string
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string

	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config.
	context string
	// readinessTimeout is maximum time to wait for all Istio resources to be ready.
	readinessTimeout time.Duration
	// wait is flag that indicates whether to wait resources ready before exiting.
	wait bool
}

const (
	istioControllerComponentName = "Operator"
	istioNamespaceComponentName  = "IstioNamespace"
	istioOperatorCRComponentName = "OperatorCustomResource"
)

// manifestApplier is used for test dependency injection.
type manifestApplier func(manifestStr, componentName string, opts *kubectlcmd.Options, verbose bool, l *Logger) bool

var (
	defaultManifestApplier = applyManifest
)

func addOperatorInitFlags(cmd *cobra.Command, args *operatorInitArgs) {
	hub, tag := buildversion.DockerInfo.Hub, buildversion.DockerInfo.Tag
	if hub == "" {
		hub = "gcr.io/istio-testing"
	}
	if tag == "" {
		tag = "latest"
	}
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use")
	cmd.PersistentFlags().DurationVar(&args.readinessTimeout, "readiness-timeout", 300*time.Second, "Maximum seconds to wait for all Istio resources to be ready."+
		" The --wait flag must be set for this flag to apply")
	cmd.PersistentFlags().BoolVarP(&args.wait, "wait", "w", false, "Wait, if set will wait until all Pods, Services, and minimum number of Pods "+
		"of a Deployment are in a ready state before the command exits. It will wait for a maximum duration of --readiness-timeout seconds")

	cmd.PersistentFlags().StringVar(&args.hub, "hub", hub, "The hub for the operator controller image")
	cmd.PersistentFlags().StringVar(&args.tag, "tag", tag, "The tag for the operator controller image")
	cmd.PersistentFlags().StringVar(&args.operatorNamespace, "operatorNamespace", "istio-operator",
		"The namespace the operator controller is installed into")
	cmd.PersistentFlags().StringVar(&args.istioNamespace, "istioNamespace", "istio-system",
		"The namespace Istio is installed into")
}

func operatorInitCmd(rootArgs *rootArgs, oiArgs *operatorInitArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Installs the Istio operator controller in the cluster.",
		Long:  "The init subcommand installs the Istio operator controller in the cluster.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			operatorInit(rootArgs, oiArgs, l, defaultManifestApplier)
		}}
}

// operatorInit installs the Istio operator controller into the cluster.
func operatorInit(args *rootArgs, oiArgs *operatorInitArgs, l *Logger, apply manifestApplier) {
	initLogsOrExit(args)

	// Error here likely indicates Deployment is missing. If some other K8s error, we will hit it again later.
	already, _ := isControllerInstalled(oiArgs.kubeConfigPath, oiArgs.context, oiArgs.operatorNamespace)
	if already {
		l.logAndPrintf("Operator controller is already installed in %s namespace, updating.", oiArgs.operatorNamespace)
	}

	l.logAndPrintf("Using operator Deployment image: %s/operator:%s", oiArgs.hub, oiArgs.tag)

	mstr, err := renderOperatorManifest(args, oiArgs, l)
	if err != nil {
		l.logAndFatal(err)
	}

	log.Infof("Using the following manifest to install operator:\n%s\n", mstr)

	// If CR was passed, we must create a namespace for it and install CR into it.
	customResource, istioNamespace, err := getCRAndNamespaceFromFile(oiArgs.inFilename, l)
	if err != nil {
		l.logAndFatal(err)
	}

	opts := &kubectlcmd.Options{
		DryRun:      args.dryRun,
		Verbose:     args.verbose,
		WaitTimeout: 1 * time.Minute,
		Kubeconfig:  oiArgs.kubeConfigPath,
		Context:     oiArgs.context,
	}

	if err := manifest.InitK8SRestClient(opts.Kubeconfig, opts.Context); err != nil {
		l.logAndFatal(err)
	}

	success := apply(mstr, istioControllerComponentName, opts, args.verbose, l)

	if customResource != "" {
		success = success && apply(genNamespaceResource(istioNamespace), istioNamespaceComponentName, opts, args.verbose, l)
		success = success && apply(customResource, istioOperatorCRComponentName, opts, args.verbose, l)
	}

	if !success {
		l.logAndPrint("\n*** Errors were logged during apply operation. Please check component installation logs above. ***\n")
		return
	}

	l.logAndPrint("\n*** Success. ***\n")
}

func applyManifest(manifestStr, componentName string, opts *kubectlcmd.Options, verbose bool, l *Logger) bool {
	l.logAndPrint("")
	// Specifically don't prune operator installation since it leads to a lot of resources being reapplied.
	opts.Prune = pointer.BoolPtr(false)
	out, objs := manifest.ApplyManifest(name.ComponentName(componentName), manifestStr, version.OperatorBinaryVersion.String(), *opts)

	success := true
	if out.Err != nil {
		cs := fmt.Sprintf("Component %s install returned the following errors:", componentName)
		l.logAndPrintf("\n%s\n%s", cs, strings.Repeat("=", len(cs)))
		l.logAndPrint("Error: ", out.Err, "\n")
		success = false
	} else {
		l.logAndPrintf("Component %s installed successfully.", componentName)
		if opts.Verbose {
			l.logAndPrintf("The following objects were installed:\n%s", k8sObjectsString(objs))
		}
	}

	if !ignoreError(out.Stderr) {
		l.logAndPrint("Error detail:\n", out.Stderr, "\n")
		success = false
	}
	if !ignoreError(out.Stderr) {
		l.logAndPrint(out.Stdout, "\n")
	}
	return success
}

func getCRAndNamespaceFromFile(filePath string, l *Logger) (customResource string, istioNamespace string, err error) {
	if filePath == "" {
		return "", "", nil
	}

	mergedYAML, err := genProfile(false, filePath, "", "", "", true, l)
	if err != nil {
		return "", "", err
	}
	mergedICPS, err := unmarshalAndValidateICPS(mergedYAML, true, l)
	if err != nil {
		return "", "", err
	}

	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", "", fmt.Errorf("could not read values from file %s: %s", filePath, err)
	}
	customResource = string(b)
	istioNamespace = mergedICPS.DefaultNamespace
	return
}

// chartsRootDir, helmBaseDir, componentName, namespace string) (TemplateRenderer, error) {
func renderOperatorManifest(_ *rootArgs, oiArgs *operatorInitArgs, _ *Logger) (string, error) {
	r, err := helm.NewHelmRenderer("", "../operator", istioControllerComponentName, oiArgs.operatorNamespace)
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
		OperatorNamespace: oiArgs.operatorNamespace,
		IstioNamespace:    oiArgs.istioNamespace,
		Hub:               oiArgs.hub,
		Tag:               oiArgs.tag,
	}
	vals, err := util.RenderTemplate(tmpl, tv)
	if err != nil {
		return "", err
	}
	log.Infof("Installing operator charts with the following values:\n%s", vals)
	return r.RenderManifest(vals)
}

func genNamespaceResource(namespace string) string {
	tmpl := `
apiVersion: v1
kind: Namespace
metadata:
  labels:
    istio-injection: disabled
  name: {{.Namespace}}
`

	tv := struct {
		Namespace string
	}{
		Namespace: namespace,
	}
	vals, err := util.RenderTemplate(tmpl, tv)
	if err != nil {
		return ""
	}
	return vals
}

func k8sObjectsString(objs object.K8sObjects) string {
	var out []string
	for _, o := range objs {
		out = append(out, fmt.Sprintf("- %s/%s/%s", o.Kind, o.Namespace, o.Name))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}
