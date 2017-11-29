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

package component

import (
	"os"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
	"log"
)

type CommonComp struct {
	framework.Component
	Name    string
	LogFile string
}

func (cc *CommonComp) GetName() string {
	return cc.Name
}

type CommonProcessComp struct {
	CommonComp
	Process *os.Process
	BinaryPath string
}

func (cpc *CommonProcessComp) Stop() (err error) {
	log.Printf("Stopping component %s", cpc.GetName())
	err = util.KillProcess(cpc.Process)
	return
}
