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
	"github.com/hashicorp/go-multierror"

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
		return fmt.Errorf("nil EnvoyExternalAuthorizationHttpProvider")
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
			errs = appendErrors(errs, fmt.Errorf("pathPrefix should begin with `/` but found %q", config.PathPrefix))
		}
	}
	return
}

func validateExtensionProviderEnvoyExtAuthzGRPC(config *meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider) (errs error) {
	if config == nil {
		return fmt.Errorf("nil EnvoyExternalAuthorizationGrpcProvider")
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
	definedProviders := map[string]struct{}{}
	for _, c := range config.ExtensionProviders {
		var currentErrs error
		// Provider name must be unique and not empty.
		if c.Name == "" {
			currentErrs = appendErrors(currentErrs, fmt.Errorf("empty extension provider name"))
		} else {
			if _, found := definedProviders[c.Name]; found {
				currentErrs = appendErrors(currentErrs, fmt.Errorf("duplicate extension provider name %s", c.Name))
			}
			definedProviders[c.Name] = struct{}{}
		}

		switch provider := c.Provider.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp:
			currentErrs = appendErrors(currentErrs, validateExtensionProviderEnvoyExtAuthzHTTP(provider.EnvoyExtAuthzHttp))
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc:
			currentErrs = appendErrors(currentErrs, validateExtensionProviderEnvoyExtAuthzGRPC(provider.EnvoyExtAuthzGrpc))
		default:
			currentErrs = appendErrors(currentErrs, fmt.Errorf("unsupported provider: %v", provider))
		}
		currentErrs = multierror.Prefix(currentErrs, fmt.Sprintf("invalid extension provider %s:", c.Name))
		errs = appendErrors(errs, currentErrs)
	}
	return
}
