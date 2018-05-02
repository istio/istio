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

package common

import (
	"fmt"
	"strings"
)

// Uniquifier makes unique identifiers within a given scope.
// TODO: Better name
type Uniquifier struct {
	used map[string]bool
}

// NewUniquifier returns a new instance of Uniquifier.
func NewUniquifier() *Uniquifier {
	return &Uniquifier{
		used: make(map[string]bool),
	}
}

// Mangled returns a unique identifier, mangled from the given parts, and discriminated if necessary.
func (u *Uniquifier) Mangled(parts ...string) string {
	prefix := strings.Join(parts, "_")
	return u.Discriminated(prefix, false)
}

// Discriminated returns a unique identifier that is discriminated by a suffix, if necessary.
func (u *Uniquifier) Discriminated(identifier string, always bool) string {
	name := identifier
	discriminator := 0

	if always {
		name = discriminate(identifier, &discriminator)
	}

	for {
		if _, contains := u.used[name]; !contains {
			u.used[name] = true
			return name
		}

		name = discriminate(identifier, &discriminator)
	}
}

func discriminate(identifier string, discriminator *int) string {
	s := fmt.Sprintf("%s%d", identifier, *discriminator)
	*discriminator++
	return s
}
