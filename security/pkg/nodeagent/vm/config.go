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

package vm

import (
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient"
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
	// Config for CA Client
	CAClientConfig caclient.Config

	// LoggingOptions is the options for Istio logging.
	LoggingOptions *log.Options

	// SecretDirectory is the directory to to save keys/certs when using file mode SecretServer.
	SecretDirectory string
}

// NewConfig creates a new Config instance with default values.
func NewConfig() *Config {
	return &Config{
		CAClientConfig: caclient.Config{
			CSRInitialRetrialInterval: defaultCSRInitialRetrialInterval,
			CSRMaxRetries:             defaultCSRMaxRetries,
			CSRGracePeriodPercentage:  defaultCSRGracePeriodPercentage,
		},
		LoggingOptions: log.DefaultOptions(),
	}
}
