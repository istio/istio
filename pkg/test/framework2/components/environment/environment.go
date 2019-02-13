//  Copyright 2019 Istio Authors
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

package environment

import (
	"fmt"
)

const (
	NameNative = "native"
	NameKube = "kube"
)
func Names() []string {
	return []string {
		NameNative,
		NameKube,
	}
}

func DefaultName() string {
	return NameNative
}

type FactoryFn func(string, Context) (Instance, error)

// UnsupportedEnvironment generates an error indicating that the given environment is not supported.
func UnsupportedEnvironment(name string) error {
	return fmt.Errorf("unsupported environment: %q", name)
}

// Instance of environment.
type Instance interface {
	Name() string
}

// Context for environments
type Context interface {
	CreateTmpDirectory(string) (string, error)
}