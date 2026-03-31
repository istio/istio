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

package config

import (
	"strings"
)

func Split(s string) []string {
	if s == "" {
		return nil
	}
	return filterEmpty(strings.Split(s, ","))
}

func filterEmpty(strs []string) []string {
	filtered := make([]string, 0, len(strs))
	for _, s := range strs {
		if s == "" {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

type InterceptFilter struct {
	Values []string
	Except bool
}

func ParseInterceptFilter(include, exclude string) InterceptFilter {
	if include == "*" {
		excludes := Split(exclude)
		return InterceptAllExcept(excludes...)
	}
	includes := Split(include)
	return InterceptOnly(includes...)
}

func InterceptAllExcept(values ...string) InterceptFilter {
	return InterceptFilter{
		Values: values,
		Except: true,
	}
}

func InterceptOnly(values ...string) InterceptFilter {
	return InterceptFilter{
		Values: values,
		Except: false,
	}
}
