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

import "fmt"

type Uniquifier struct {
	used map[string]bool
}

func NewUniquifier() *Uniquifier {
	return &Uniquifier{
		used: make(map[string]bool),
	}
}

func (u *Uniquifier) Add(name string) {
	u.used[name] = true
}

func (u *Uniquifier) Generate(prefix string) string {
	name := prefix
	discriminator := 0

	for {
		name = fmt.Sprintf("%s%d", prefix, discriminator)
		discriminator++

		if _, ok := u.used[name]; !ok {
			u.used[name] = true
			return name
		}
	}
}

