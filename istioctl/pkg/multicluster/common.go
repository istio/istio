// Copyright 2019 Istio Authors.
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
)

const (
	defaultIstioNamespace       = "istio-system"
	defaultServiceAccountReader = "istio-multi"
)

// TODO(ayj) - add to istio.io/api/annotations
const clusterContextAnnotationKey = "istio.io/clusterContext"

// KubeOptions contains kubernetes options common to all commands.
type KubeOptions struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

// Inherit the common kubernetes flags defined in the root package. This is a bit of a hack,
// but it allows us to directly get the final values for each of these flags without needing
// to pass pointers-to-flags through all of the (sub)commands.
func (o *KubeOptions) prepare(flags *pflag.FlagSet) error {
	if f := flags.Lookup("Kubeconfig"); f != nil {
		o.Kubeconfig = f.Value.String()
	}
	if f := flags.Lookup("Context"); f != nil {
		o.Context = f.Value.String()
	}
	if f := flags.Lookup("Namespace"); f != nil {
		o.Namespace = f.Value.String()
	}
	return nil
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
