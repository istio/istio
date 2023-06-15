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

package root

import (
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
)

const (
	// Location to read istioctl defaults from
	defaultIstioctlConfig = "$HOME/.istioctl/config.yaml"
)

var (
	// IstioConfig is the name of the istioctl config file (if any)
	IstioConfig = env.Register("ISTIOCONFIG", defaultIstioctlConfig,
		"Default values for istioctl flags").Get()

	LoggingOptions = defaultLogOptions()

	// scope is for dev logging.  Warning: log levels are not set by --log_output_level until command is Run().
	Scope = log.RegisterScope("cli", "istioctl")
)

func defaultLogOptions() *log.Options {
	o := log.DefaultOptions()

	// These scopes are, at the default "INFO" level, too chatty for command line use
	o.SetOutputLevel("validation", log.ErrorLevel)
	o.SetOutputLevel("processing", log.ErrorLevel)
	o.SetOutputLevel("analysis", log.WarnLevel)
	o.SetOutputLevel("installer", log.WarnLevel)
	o.SetOutputLevel("translator", log.WarnLevel)
	o.SetOutputLevel("adsc", log.WarnLevel)
	o.SetOutputLevel("default", log.WarnLevel)
	o.SetOutputLevel("klog", log.WarnLevel)
	o.SetOutputLevel("kube", log.ErrorLevel)

	return o
}
