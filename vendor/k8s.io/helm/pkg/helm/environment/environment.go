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

/*Package environment describes the operating environment for Tiller.

Tiller's environment encapsulates all of the service dependencies Tiller has.
These dependencies are expressed as interfaces so that alternate implementations
(mocks, etc.) can be easily generated.
*/
package environment

import (
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"k8s.io/client-go/util/homedir"
	"k8s.io/helm/pkg/helm/helmpath"
)

const (
	// DefaultTLSCaCert is the default value for HELM_TLS_CA_CERT
	DefaultTLSCaCert = "$HELM_HOME/ca.pem"
	// DefaultTLSCert is the default value for HELM_TLS_CERT
	DefaultTLSCert = "$HELM_HOME/cert.pem"
	// DefaultTLSKeyFile is the default value for HELM_TLS_KEY_FILE
	DefaultTLSKeyFile = "$HELM_HOME/key.pem"
	// DefaultTLSEnable is the default value for HELM_TLS_ENABLE
	DefaultTLSEnable = false
	// DefaultTLSVerify is the default value for HELM_TLS_VERIFY
	DefaultTLSVerify = false
)

// DefaultHelmHome is the default HELM_HOME.
var DefaultHelmHome = filepath.Join(homedir.HomeDir(), ".helm")

// EnvSettings describes all of the environment settings.
type EnvSettings struct {
	// TillerHost is the host and port of Tiller.
	TillerHost string
	// TillerConnectionTimeout is the duration (in seconds) helm will wait to establish a connection to Tiller.
	TillerConnectionTimeout int64
	// TillerNamespace is the namespace in which Tiller runs.
	TillerNamespace string
	// Home is the local path to the Helm home directory.
	Home helmpath.Home
	// Debug indicates whether or not Helm is running in Debug mode.
	Debug bool
	// KubeContext is the name of the kubeconfig context.
	KubeContext string
	// KubeConfig is the path to an explicit kubeconfig file. This overwrites the value in $KUBECONFIG
	KubeConfig string
	// TLSEnable tells helm to communicate with Tiller via TLS
	TLSEnable bool
	// TLSVerify tells helm to communicate with Tiller via TLS and to verify remote certificates served by Tiller
	TLSVerify bool
	// TLSServerName tells helm to verify the hostname on the returned certificates from Tiller
	TLSServerName string
	// TLSCaCertFile is the path to a TLS CA certificate file
	TLSCaCertFile string
	// TLSCertFile is the path to a TLS certificate file
	TLSCertFile string
	// TLSKeyFile is the path to a TLS key file
	TLSKeyFile string
}

// AddFlags binds flags to the given flagset.
func (s *EnvSettings) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar((*string)(&s.Home), "home", DefaultHelmHome, "location of your Helm config. Overrides $HELM_HOME")
	fs.StringVar(&s.TillerHost, "host", "", "address of Tiller. Overrides $HELM_HOST")
	fs.StringVar(&s.KubeContext, "kube-context", "", "name of the kubeconfig context to use")
	fs.StringVar(&s.KubeConfig, "kubeconfig", "", "absolute path to the kubeconfig file to use")
	fs.BoolVar(&s.Debug, "debug", false, "enable verbose output")
	fs.StringVar(&s.TillerNamespace, "tiller-namespace", "kube-system", "namespace of Tiller")
	fs.Int64Var(&s.TillerConnectionTimeout, "tiller-connection-timeout", int64(300), "the duration (in seconds) Helm will wait to establish a connection to tiller")
}

// AddFlagsTLS adds the flags for supporting client side TLS to the given flagset.
func (s *EnvSettings) AddFlagsTLS(fs *pflag.FlagSet) {
	fs.StringVar(&s.TLSServerName, "tls-hostname", s.TillerHost, "the server name used to verify the hostname on the returned certificates from the server")
	fs.StringVar(&s.TLSCaCertFile, "tls-ca-cert", DefaultTLSCaCert, "path to TLS CA certificate file")
	fs.StringVar(&s.TLSCertFile, "tls-cert", DefaultTLSCert, "path to TLS certificate file")
	fs.StringVar(&s.TLSKeyFile, "tls-key", DefaultTLSKeyFile, "path to TLS key file")
	fs.BoolVar(&s.TLSVerify, "tls-verify", DefaultTLSVerify, "enable TLS for request and verify remote")
	fs.BoolVar(&s.TLSEnable, "tls", DefaultTLSEnable, "enable TLS for request")
}

// Init sets values from the environment.
func (s *EnvSettings) Init(fs *pflag.FlagSet) {
	for name, envar := range envMap {
		setFlagFromEnv(name, envar, fs)
	}
}

// InitTLS sets TLS values from the environment.
func (s *EnvSettings) InitTLS(fs *pflag.FlagSet) {
	for name, envar := range tlsEnvMap {
		setFlagFromEnv(name, envar, fs)
	}
}

// envMap maps flag names to envvars
var envMap = map[string]string{
	"debug":            "HELM_DEBUG",
	"home":             "HELM_HOME",
	"host":             "HELM_HOST",
	"tiller-namespace": "TILLER_NAMESPACE",
}

var tlsEnvMap = map[string]string{
	"tls-hostname": "HELM_TLS_HOSTNAME",
	"tls-ca-cert":  "HELM_TLS_CA_CERT",
	"tls-cert":     "HELM_TLS_CERT",
	"tls-key":      "HELM_TLS_KEY",
	"tls-verify":   "HELM_TLS_VERIFY",
	"tls":          "HELM_TLS_ENABLE",
}

// PluginDirs is the path to the plugin directories.
func (s EnvSettings) PluginDirs() string {
	if d, ok := os.LookupEnv("HELM_PLUGIN"); ok {
		return d
	}
	return s.Home.Plugins()
}

// HelmKeyPassphrase is the passphrase used to sign a helm chart.
func (s EnvSettings) HelmKeyPassphrase() string {
	if d, ok := os.LookupEnv("HELM_KEY_PASSPHRASE"); ok {
		return d
	}
	return ""
}

// setFlagFromEnv looks up and sets a flag if the corresponding environment variable changed.
// if the flag with the corresponding name was set during fs.Parse(), then the environment
// variable is ignored.
func setFlagFromEnv(name, envar string, fs *pflag.FlagSet) {
	if fs.Changed(name) {
		return
	}
	if v, ok := os.LookupEnv(envar); ok {
		fs.Set(name, v)
	}
}

// Deprecated
const (
	HomeEnvVar          = "HELM_HOME"
	PluginEnvVar        = "HELM_PLUGIN"
	PluginDisableEnvVar = "HELM_NO_PLUGINS"
	HostEnvVar          = "HELM_HOST"
	DebugEnvVar         = "HELM_DEBUG"
)
