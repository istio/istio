/*
Copyright 2016 The Kubernetes Authors.

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

package envtest

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/testing_frameworks/integration"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.KBLog.WithName("test-env")

// Default binary path for test framework
const (
	envKubeAPIServerBin    = "TEST_ASSET_KUBE_APISERVER"
	envEtcdBin             = "TEST_ASSET_ETCD"
	envKubectlBin          = "TEST_ASSET_KUBECTL"
	envKubebuilderPath     = "KUBEBUILDER_ASSETS"
	envStartTimeout        = "KUBEBUILDER_CONTROLPLANE_START_TIMEOUT"
	envStopTimeout         = "KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT"
	defaultKubebuilderPath = "/usr/local/kubebuilder/bin"
	StartTimeout           = 60
	StopTimeout            = 60

	defaultKubebuilderControlPlaneStartTimeout = 20 * time.Second
	defaultKubebuilderControlPlaneStopTimeout  = 20 * time.Second
)

func defaultAssetPath(binary string) string {
	assetPath := os.Getenv(envKubebuilderPath)
	if assetPath == "" {
		assetPath = defaultKubebuilderPath
	}
	return filepath.Join(assetPath, binary)

}

// DefaultKubeAPIServerFlags are default flags necessary to bring up apiserver.
var DefaultKubeAPIServerFlags = []string{
	"--etcd-servers={{ if .EtcdURL }}{{ .EtcdURL.String }}{{ end }}",
	"--cert-dir={{ .CertDir }}",
	"--insecure-port={{ if .URL }}{{ .URL.Port }}{{ end }}",
	"--insecure-bind-address={{ if .URL }}{{ .URL.Hostname }}{{ end }}",
	"--secure-port={{ if .SecurePort }}{{ .SecurePort }}{{ end }}",
	"--admission-control=AlwaysAdmit",
}

// Environment creates a Kubernetes test environment that will start / stop the Kubernetes control plane and
// install extension APIs
type Environment struct {
	// ControlPlane is the ControlPlane including the apiserver and etcd
	ControlPlane integration.ControlPlane

	// Config can be used to talk to the apiserver
	Config *rest.Config

	// CRDs is a list of CRDs to install
	CRDs []*apiextensionsv1beta1.CustomResourceDefinition

	// CRDDirectoryPaths is a list of paths containing CRD yaml or json configs.
	CRDDirectoryPaths []string

	// UseExisting indicates that this environments should use an
	// existing kubeconfig, instead of trying to stand up a new control plane.
	// This is useful in cases that need aggregated API servers and the like.
	UseExistingCluster bool

	// ControlPlaneStartTimeout is the maximum duration each controlplane component
	// may take to start. It defaults to the KUBEBUILDER_CONTROLPLANE_START_TIMEOUT
	// environment variable or 20 seconds if unspecified
	ControlPlaneStartTimeout time.Duration

	// ControlPlaneStopTimeout is the maximum duration each controlplane component
	// may take to stop. It defaults to the KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT
	// environment variable or 20 seconds if unspecified
	ControlPlaneStopTimeout time.Duration

	// KubeAPIServerFlags is the set of flags passed while starting the api server.
	KubeAPIServerFlags []string
}

// Stop stops a running server
func (te *Environment) Stop() error {
	if te.UseExistingCluster {
		return nil
	}
	return te.ControlPlane.Stop()
}

// getAPIServerFlags returns flags to be used with the Kubernetes API server.
func (te Environment) getAPIServerFlags() []string {
	// Set default API server flags if not set.
	if len(te.KubeAPIServerFlags) == 0 {
		return DefaultKubeAPIServerFlags
	}
	return te.KubeAPIServerFlags
}

// Start starts a local Kubernetes server and updates te.ApiserverPort with the port it is listening on
func (te *Environment) Start() (*rest.Config, error) {
	if te.UseExistingCluster {
		log.V(1).Info("using existing cluster")
		if te.Config == nil {
			// we want to allow people to pass in their own config, so
			// only load a config if it hasn't already been set.
			log.V(1).Info("automatically acquiring client configuration")

			var err error
			te.Config, err = config.GetConfig()
			if err != nil {
				return nil, err
			}
		}
	} else {
		te.ControlPlane = integration.ControlPlane{}
		te.ControlPlane.APIServer = &integration.APIServer{Args: te.getAPIServerFlags()}
		te.ControlPlane.Etcd = &integration.Etcd{}

		if os.Getenv(envKubeAPIServerBin) == "" {
			te.ControlPlane.APIServer.Path = defaultAssetPath("kube-apiserver")
		}
		if os.Getenv(envEtcdBin) == "" {
			te.ControlPlane.Etcd.Path = defaultAssetPath("etcd")
		}
		if os.Getenv(envKubectlBin) == "" {
			// we can't just set the path manually (it's behind a function), so set the environment variable instead
			if err := os.Setenv(envKubectlBin, defaultAssetPath("kubectl")); err != nil {
				return nil, err
			}
		}

		if err := te.defaultTimeouts(); err != nil {
			return nil, fmt.Errorf("failed to default controlplane timeouts: %v", err)
		}
		te.ControlPlane.Etcd.StartTimeout = te.ControlPlaneStartTimeout
		te.ControlPlane.Etcd.StopTimeout = te.ControlPlaneStopTimeout
		te.ControlPlane.APIServer.StartTimeout = te.ControlPlaneStartTimeout
		te.ControlPlane.APIServer.StopTimeout = te.ControlPlaneStopTimeout

		log.V(1).Info("starting control plane", "api server flags", te.ControlPlane.APIServer.Args)
		if err := te.startControlPlane(); err != nil {
			return nil, err
		}

		// Create the *rest.Config for creating new clients
		te.Config = &rest.Config{
			Host: te.ControlPlane.APIURL().Host,
		}
	}

	log.V(1).Info("installing CRDs")
	_, err := InstallCRDs(te.Config, CRDInstallOptions{
		Paths: te.CRDDirectoryPaths,
		CRDs:  te.CRDs,
	})
	return te.Config, err
}

func (te *Environment) startControlPlane() error {
	numTries, maxRetries := 0, 5
	var err error
	for ; numTries < maxRetries; numTries++ {
		// Start the control plane - retry if it fails
		err = te.ControlPlane.Start()
		if err == nil {
			break
		}
		log.Error(err, "unable to start the controlplane", "tries", numTries)
	}
	if numTries == maxRetries {
		return fmt.Errorf("failed to start the controlplane. retried %d times: %v", numTries, err)
	}
	return nil
}

func (te *Environment) defaultTimeouts() error {
	var err error
	if te.ControlPlaneStartTimeout == 0 {
		if envVal := os.Getenv(envStartTimeout); envVal != "" {
			te.ControlPlaneStartTimeout, err = time.ParseDuration(envVal)
			if err != nil {
				return err
			}
		} else {
			te.ControlPlaneStartTimeout = defaultKubebuilderControlPlaneStartTimeout
		}
	}

	if te.ControlPlaneStopTimeout == 0 {
		if envVal := os.Getenv(envStopTimeout); envVal != "" {
			te.ControlPlaneStopTimeout, err = time.ParseDuration(envVal)
			if err != nil {
				return err
			}
		} else {
			te.ControlPlaneStopTimeout = defaultKubebuilderControlPlaneStopTimeout
		}
	}
	return nil
}
