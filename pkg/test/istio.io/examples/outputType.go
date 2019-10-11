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
package examples

import (
	"io/ioutil"
	"path"
)

type outputTypeImpl interface {
	Write(name string, output []byte)
}

type textOutput struct{}

func (textOutput) Write(name string, output []byte) {
	_, filename := path.Split(name)
	ioutil.WriteFile(path.Join("output/", filename+"_output.txt"), output, 0644)

}

type yamlOutput struct{}

func (yamlOutput) Write(name string, output []byte) {
	_, filename := path.Split(name)
	ioutil.WriteFile(path.Join("output/", filename+"_output.yaml"), output, 0644)
}

type jsonOutput struct{}

func (jsonOutput) Write(name string, output []byte) {
	_, filename := path.Split(name)
	ioutil.WriteFile(path.Join("output/", filename+"_output.json"), output, 0644)
}
