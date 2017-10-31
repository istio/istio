// Copyright 2017 Istio Authors
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

package na

import (
	"time"

	"istio.io/auth/pkg/platform"
)

const (
	// defaultCSRInitialRetrialInterval is the default value of Config.CSRInitialRetrialInterval.
	defaultCSRInitialRetrialInterval = time.Second * 5
	// defaultCSRMaxRetries is the default value of Config.CSRMaxRetries.
	defaultCSRMaxRetries = 5
	// defaultCSRGracePeriodPercentage is the default value of Config.CSRGracePeriodPercentage.
	defaultCSRGracePeriodPercentage = 50
)

// Config is Node agent configuration.
type Config struct {
	// Istio CA grpc server
	IstioCAAddress string

	// Organization of service
	ServiceIdentityOrg string

	RSAKeySize int

	// The environment this node agent is running on
	Env string

	// CSRInitialRetrialInterval is the retrial interval for certificate requests.
	CSRInitialRetrialInterval time.Duration

	// CSRMaxRetries is the number of retries for certificate requests.
	CSRMaxRetries int

	// CSRGracePeriodPercentage indicates the length of the grace period in the
	// percentage of the entire certificate TTL.
	CSRGracePeriodPercentage int

	// The Configuration for talking to the platform metadata server.
	PlatformConfig platform.ClientConfig
}

// InitializeConfig initializes Config with default values.
func InitializeConfig(config *Config) {
	config.CSRInitialRetrialInterval = defaultCSRInitialRetrialInterval
	config.CSRMaxRetries = defaultCSRMaxRetries
	config.CSRGracePeriodPercentage = defaultCSRGracePeriodPercentage
	config.PlatformConfig = platform.ClientConfig{}
}
