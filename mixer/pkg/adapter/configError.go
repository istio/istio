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

package adapter

import (
	"fmt"

	me "github.com/hashicorp/go-multierror"
)

// ConfigErrors is a collection of configuration errors
//
// The usage pattern for this type is pretty simple:
//
//   func (a *adapterState) ValidateConfig(cfg adapter.AspectConfig) (ce *adapter.ConfigErrors) {
//       c := cfg.(*Config)
//       if c.Url == nil {
//           ce = ce.Appendf("url", "Must have a valid URL")
//       }
//       if c.RetryCount < 0 {
//           ce = ce.Appendf("retryCount", "Expecting >= 0, got %d", cfg.RetryCount)
//       }
//       return
//    }
type ConfigErrors struct {
	// Multi is the accumulator of errors
	Multi *me.Error
}

// ConfigError represents an error encountered while validating a block of configuration state.
type ConfigError struct {
	Field      string
	Underlying error
}

// Appendf adds a ConfigError to a multierror. This function is intended
// to be used by adapter's ValidateConfig method to report errors
// in configuration. The field parameter indicates the name of the
// specific configuration field name that is problematic.
func (e *ConfigErrors) Appendf(field, format string, args ...interface{}) *ConfigErrors {
	ce := e
	if ce == nil {
		ce = &ConfigErrors{}
	}

	ce.Multi = me.Append(ce.Multi, ConfigError{field, fmt.Errorf(format, args...)})
	return ce
}

// Append adds a ConfigError to a multierror. This function is intended
// to be used by adapter's ValidateConfig method to report errors
// in configuration. The field parameter indicates the name of the
// specific configuration field name that is problematic.
func (e *ConfigErrors) Append(field string, err error) *ConfigErrors {
	ce := e
	if ce == nil {
		ce = &ConfigErrors{}
	}

	ce.Multi = me.Append(ce.Multi, ConfigError{field, err})
	return ce
}

// Error returns a string representation of the configuration error.
func (e ConfigError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Underlying)
}

func (e ConfigError) String() string {
	return e.Error()
}

func (e *ConfigErrors) Error() string {
	if e == nil || e.Multi == nil {
		return ""
	}
	return e.Multi.Error()
}

func (e *ConfigErrors) String() string {
	return e.Error()
}

// Extend joins 2 configErrors together.
func (e *ConfigErrors) Extend(ee *ConfigErrors) *ConfigErrors {
	if e == nil {
		return ee
	}
	if ee != nil {
		e.Multi = me.Append(e.Multi, ee.Multi.Errors...)
	}
	return e
}
