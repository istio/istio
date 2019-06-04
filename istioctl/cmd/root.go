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

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/istioctl/cmd/istioctl/gendeployment"
	"istio.io/istio/istioctl/pkg/install"
	"istio.io/istio/istioctl/pkg/validate"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cmd"
	"istio.io/pkg/collateral"
	"istio.io/pkg/log"
)

var (
	kubeconfig       string
	configContext    string
	namespace        string
	istioNamespace   string
	defaultNamespace string

	// input file name
	file string

	// output format (yaml or short)
	outputFormat string

	// Create a kubernetes.ExecClient (or mockExecClient)
	clientExecFactory = newExecClient

	loggingOptions = log.DefaultOptions()
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               "istioctl",
		Short:             "Istio control interface.",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Long: `Istio configuration command line utility for service operators to
debug and diagnose their Istio mesh.
`,
		PersistentPreRunE: istioPersistentPreRunE,
	}

	rootCmd.SetArgs(args)

	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "c", "",
		"Kubernetes configuration file")

	rootCmd.PersistentFlags().StringVar(&configContext, "context", "",
		"The name of the kubeconfig context to use")

	rootCmd.PersistentFlags().StringVarP(&istioNamespace, "istioNamespace", "i", kube.IstioNamespace,
		"Istio system namespace")

	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", v1.NamespaceAll,
		"Config namespace")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)
	hiddenFlags := []string{"log_as_json", "log_rotate", "log_rotate_max_age", "log_rotate_max_backups",
		"log_rotate_max_size", "log_stacktrace_level", "log_target", "log_caller"}
	for _, opt := range hiddenFlags {
		_ = rootCmd.PersistentFlags().MarkHidden(opt)
	}

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(gendeployment.Command(&istioNamespace))
	rootCmd.AddCommand(AuthN())
	rootCmd.AddCommand(register())
	rootCmd.AddCommand(deregisterCmd)
	rootCmd.AddCommand(injectCommand())

	experimentalCmd := &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"x", "exp"},
		Short:   "Experimental commands that may be modified or deprecated",
	}

	rootCmd.AddCommand(experimentalCmd)
	rootCmd.AddCommand(proxyConfig())
	rootCmd.AddCommand(statusCmd)

	rootCmd.AddCommand(install.NewVerifyCommand())
	experimentalCmd.AddCommand(Auth())
	experimentalCmd.AddCommand(convertIngress())
	experimentalCmd.AddCommand(dashboard())
	experimentalCmd.AddCommand(metricsCmd)

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Control",
		Section: "istioctl CLI",
		Manual:  "Istio Control",
	}))

	// Deprecated commands
	rootCmd.AddCommand(postCmd)
	rootCmd.AddCommand(putCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(contextCmd)

	rootCmd.AddCommand(validate.NewValidateCommand(&istioNamespace))

	return rootCmd
}

func istioPersistentPreRunE(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	defaultNamespace = getDefaultNamespace(kubeconfig)
	return nil
}

func getDefaultNamespace(kubeconfig string) string {
	configAccess := clientcmd.NewDefaultPathOptions()

	if kubeconfig != "" {
		// use specified kubeconfig file for the location of the
		// config to read
		configAccess.GlobalFile = kubeconfig
	}

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		return v1.NamespaceDefault
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return v1.NamespaceDefault
	}
	if context.Namespace == "" {
		return v1.NamespaceDefault
	}
	return context.Namespace
}
