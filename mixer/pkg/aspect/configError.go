// Copyright 2017 Google Inc.
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

package aspect

import (
	"fmt"

	me "github.com/hashicorp/go-multierror"
)

// A collection of configuration errors
//
// The usage pattern for this type is pretty simple:
//
//  	func (a *adapterState) ValidateConfig(cfg proto.Message) (ce *adapter.ConfigErrors) {
//  		c := cfg.(*Config)
// 			if c.Url == nil {
//  			ce = ce.Appendf("Url", "Must have a valid URL")
//  		}
//  		if c.RetryCount < 0 {
//  			ce = ce.Appendf("RetryCount", "Expecting >= 0, got %d", cfg.RetryCount)
//  		}
//  		return
//  	}
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
	if e == nil {
		e = &ConfigErrors{}
	}

	e.Multi = me.Append(e.Multi, ConfigError{field, fmt.Errorf(format, args...)})
	return e
}

// Append adds a ConfigError to a multierror. This function is intended
// to be used by adapter's ValidateConfig method to report errors
// in configuration. The field parameter indicates the name of the
// specific configuration field name that is problematic.
func (e *ConfigErrors) Append(field string, err error) *ConfigErrors {
	if e == nil {
		e = &ConfigErrors{}
	}

	e.Multi = me.Append(e.Multi, ConfigError{field, err})
	return e
}

// Error returns a string representation of the configuration error.
func (e ConfigError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Underlying)
}

func (e *ConfigErrors) Error() string {
	return e.Multi.Error()
}
