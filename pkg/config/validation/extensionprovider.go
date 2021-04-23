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

package validation

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	envoytypev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func validateExtensionProviderService(service string) error {
	if service == "" {
		return fmt.Errorf("service must not be empty")
	}
	return nil
}

func validateExtensionProviderEnvoyExtAuthzStatusOnError(status string) error {
	if status == "" {
		return nil
	}
	code, err := strconv.ParseInt(status, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid statusOnError value %s: %v", status, err)
	}
	if _, found := envoytypev3.StatusCode_name[int32(code)]; !found {
		return fmt.Errorf("unsupported statusOnError value %s, supported values: %v", status, envoytypev3.StatusCode_name)
	}
	return nil
}

func validateExtensionProviderEnvoyExtAuthzHTTP(config *meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider) (errs error) {
	if config == nil {
		return fmt.Errorf("EnvoyExternalAuthorizationHttpProvider must not be nil")
	}
	if err := ValidatePort(int(config.Port)); err != nil {
		errs = appendErrors(errs, err)
	}
	if err := validateExtensionProviderService(config.Service); err != nil {
		errs = appendErrors(errs, err)
	}
	if err := validateExtensionProviderEnvoyExtAuthzStatusOnError(config.StatusOnError); err != nil {
		errs = appendErrors(errs, err)
	}
	if config.PathPrefix != "" {
		if _, err := url.Parse(config.PathPrefix); err != nil {
			errs = appendErrors(errs, fmt.Errorf("invalid pathPrefix %s: %v", config.PathPrefix, err))
		}
		if !strings.HasPrefix(config.PathPrefix, "/") {
			errs = appendErrors(errs, fmt.Errorf("pathPrefix must begin with `/` but found %q", config.PathPrefix))
		}
	}
	return
}

func validateExtensionProviderEnvoyExtAuthzGRPC(config *meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider) (errs error) {
	if config == nil {
		return fmt.Errorf("EnvoyExternalAuthorizationGrpcProvider must not be nil")
	}
	if err := ValidatePort(int(config.Port)); err != nil {
		errs = appendErrors(errs, fmt.Errorf("invalid service port: %v", err))
	}
	if err := validateExtensionProviderService(config.Service); err != nil {
		errs = appendErrors(errs, err)
	}
	if err := validateExtensionProviderEnvoyExtAuthzStatusOnError(config.StatusOnError); err != nil {
		errs = appendErrors(errs, err)
	}
	return
}

func validateExtensionProvider(config *meshconfig.MeshConfig) (errs error) {
	definedProviders := map[string]int{}
	for i, c := range config.ExtensionProviders {
		// Provider name must be unique and not empty.
		if c.Name == "" {
			errs = appendErrors(errs, fmt.Errorf("extension provider at %d: name must not be empty", i))
		} else {
			if j, found := definedProviders[c.Name]; found {
				errs = appendErrors(errs, fmt.Errorf("extension provider at %d: name (%s) must be unique, previously defined at %d", i, c.Name, j))
			} else {
				definedProviders[c.Name] = i
			}
		}

		var err error
		switch provider := c.Provider.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp:
			err = validateExtensionProviderEnvoyExtAuthzHTTP(provider.EnvoyExtAuthzHttp)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc:
			err = validateExtensionProviderEnvoyExtAuthzGRPC(provider.EnvoyExtAuthzGrpc)
		}
		if err != nil {
			errs = appendErrors(errs, fmt.Errorf("extension provider %s: %v", c.Name, err))
		}
	}
	return
}
