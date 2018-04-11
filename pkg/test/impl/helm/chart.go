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

package helm

// Represents a Helm deployment chart for testing purposes.
type Chart struct {
	name string
}

func LoadChart(name string) *Chart {
	// TODO: Load chart
	return &Chart{
		name: name,
	}
}

func (c *Chart) WithValuesFromFile(name string) *Chart {
	// TODO
	return c
}

func (c *Chart) Name() string {
	return c.name
}
