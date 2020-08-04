// Copyright Istio Authors.
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

package multicluster

import (
	"errors"

	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
)

// TODO(ayj) - add to istio.io/api/annotations
const clusterNameAnnotationKey = "networking.istio.io/cluster"

// KubeOptions contains kubernetes options common to all commands.
type KubeOptions struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

// Inherit the common kubernetes flags defined in the root package. This is a bit of a hack,
// but it allows us to directly get the final values for each of these flags without needing
// to pass pointers-to-flags through all of the (sub)commands.
func (o *KubeOptions) prepare(flags *pflag.FlagSet) {
	if f := flags.Lookup("kubeconfig"); f != nil {
		o.Kubeconfig = f.Value.String()
	}
	if f := flags.Lookup("context"); f != nil {
		o.Context = f.Value.String()
	}
	if f := flags.Lookup("namespace"); f != nil {
		o.Namespace = f.Value.String()
	}

	if o.Namespace == "" {
		o.Namespace = defaultIstioNamespace

		configAccess := clientcmd.NewDefaultPathOptions()
		configAccess.GlobalFile = o.Kubeconfig
		if config, err := configAccess.GetStartingConfig(); err == nil {
			if context, ok := config.Contexts[config.CurrentContext]; ok && context.Namespace != "" {
				o.Namespace = context.Namespace
			}
		}
	}
}

type filenameOption struct {
	filename string
}

func (f *filenameOption) addFlags(flagset *pflag.FlagSet) {
	if flagset.Lookup("filename") == nil {
		flagset.StringVarP(&f.filename, "filename", "f", "",
			"filename of the multicluster mesh description")
	}
}

func (f *filenameOption) prepare() error {
	if len(f.filename) == 0 {
		return errors.New("must specify -f")
	}
	return nil
}
