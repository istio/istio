//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package env

import "os"

// Variable is a wrapper for an environment variable.
type Variable string

// Name of the environment variable.
func (e Variable) Name() string {
	return string(e)
}

// Value of the environment variable.
func (e Variable) Value() string {
	return os.Getenv(e.Name())
}

// ValueOrDefault returns the value of the environment variable if it is non-empty. Otherwise returns the value provided.
func (e Variable) ValueOrDefault(defaultValue string) string {
	return e.ValueOrDefaultFunc(func() string {
		return defaultValue
	})
}

// ValueOrDefaultFunc returns the value of the environment variable if it is non-empty. Otherwise returns the value function provided.
func (e Variable) ValueOrDefaultFunc(defaultValueFunc func() string) string {
	if value := e.Value(); value != "" {
		return value
	}
	return defaultValueFunc()
}
