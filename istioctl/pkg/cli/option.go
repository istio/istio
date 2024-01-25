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

package cli

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/ptr"
)

const (
	FlagKubeConfig     = "kubeconfig"
	FlagContext        = "context"
	FlagNamespace      = "namespace"
	FlagIstioNamespace = "istioNamespace"
)

type RootFlags struct {
	kubeconfig     *string
	configContext  *string
	namespace      *string
	istioNamespace *string

	defaultNamespace string
}

func AddRootFlags(flags *pflag.FlagSet) *RootFlags {
	r := &RootFlags{
		kubeconfig:     ptr.Of[string](""),
		configContext:  ptr.Of[string](""),
		namespace:      ptr.Of[string](""),
		istioNamespace: ptr.Of[string](""),
	}
	flags.StringVarP(r.kubeconfig, FlagKubeConfig, "c", "",
		"Kubernetes configuration file")
	flags.StringVar(r.configContext, FlagContext, "",
		"Kubernetes configuration context")
	flags.StringVarP(r.namespace, FlagNamespace, "n", v1.NamespaceAll,
		"Kubernetes namespace")
	flags.StringVarP(r.istioNamespace, FlagIstioNamespace, "i", viper.GetString(FlagIstioNamespace),
		"Istio system namespace")
	return r
}

// Namespace returns the namespace flag value.
func (r *RootFlags) Namespace() string {
	return *r.namespace
}

// IstioNamespace returns the istioNamespace flag value.
func (r *RootFlags) IstioNamespace() string {
	return *r.istioNamespace
}

// DefaultNamespace returns the default namespace to use.
func (r *RootFlags) DefaultNamespace() string {
	if r.defaultNamespace == "" {
		r.configureDefaultNamespace()
	}
	return r.defaultNamespace
}

func (r *RootFlags) configureDefaultNamespace() {
	configAccess := clientcmd.NewDefaultPathOptions()

	kubeconfig := *r.kubeconfig
	if kubeconfig != "" {
		// use specified kubeconfig file for the location of the
		// config to read
		configAccess.GlobalFile = kubeconfig
	}

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		r.defaultNamespace = v1.NamespaceDefault
		return
	}

	// If a specific context was specified, use that. Otherwise, just use the current context from the kube config.
	selectedContext := config.CurrentContext
	if *r.configContext != "" {
		selectedContext = *r.configContext
	}

	// Use the namespace associated with the selected context as default, if the context has one
	context, ok := config.Contexts[selectedContext]
	if !ok {
		r.defaultNamespace = v1.NamespaceDefault
		return
	}
	if context.Namespace == "" {
		r.defaultNamespace = v1.NamespaceDefault
		return
	}
	r.defaultNamespace = context.Namespace
}
