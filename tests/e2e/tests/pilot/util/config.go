// Copyright 2018 Istio Authors
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

package util

import (
	"os"

	"istio.io/istio/pilot/pkg/serviceregistry"
)

const (
	defaultHub                  = "gcr.io/istio-testing"
	defaultRegistry             = string(serviceregistry.KubernetesRegistry)
	defaultAdmissionServiceName = "istio-pilot"
	defaultVerbosity            = 2
)

// Config defines the configuration for the test environment.
type Config struct {
	KubeConfig            string
	Hub                   string
	Tag                   string
	Namespace             string
	IstioNamespace        string
	Registry              string
	ErrorLogsDir          string
	CoreFilesDir          string
	SelectedTest          string
	SidecarTemplate       string
	AdmissionServiceName  string
	Verbosity             int
	DebugPort             int
	TestCount             int
	Auth                  bool
	Mixer                 bool
	Ingress               bool
	Zipkin                bool
	SkipCleanup           bool
	SkipCleanupOnFailure  bool
	CheckLogs             bool
	DebugImagesAndMode    bool
	UseAutomaticInjection bool
	V1alpha1              bool
	V1alpha2              bool
	RDSv2                 bool
	NoRBAC                bool
	UseAdmissionWebhook   bool
	APIVersions           []string
}

// NewConfig creates a new test environment configuration with default values.
func NewConfig() *Config {
	return &Config{
		KubeConfig:            os.Getenv("KUBECONFIG"),
		Hub:                   defaultHub,
		Tag:                   "",
		Namespace:             "",
		IstioNamespace:        "",
		Registry:              defaultRegistry,
		Verbosity:             defaultVerbosity,
		Auth:                  false,
		Mixer:                 true,
		Ingress:               true,
		Zipkin:                true,
		DebugPort:             0,
		SkipCleanup:           false,
		SkipCleanupOnFailure:  false,
		CheckLogs:             false,
		ErrorLogsDir:          "",
		CoreFilesDir:          "",
		TestCount:             1,
		SelectedTest:          "",
		DebugImagesAndMode:    true,
		UseAutomaticInjection: false,
		UseAdmissionWebhook:   false,
		AdmissionServiceName:  defaultAdmissionServiceName,
		V1alpha1:              false,
		V1alpha2:              true,
	}
}
