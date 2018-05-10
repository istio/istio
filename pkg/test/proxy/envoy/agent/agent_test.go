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

package agent

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
)

func TestAgent(t *testing.T) {
	a := Agent{
		Config: Config{
			ServiceName: "a",
			Ports: []PortConfig{
				{
					Name:     "http",
					Protocol: model.ProtocolHTTP,
				},
			},
		},
	}

	if err := a.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Second)

	fmt.Println("NM: Sending request")
	response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/a", a.GetPorts()[0].EnvoyPort))
	if err != nil {
		t.Fatal(err)
	}
	buf, err := ioutil.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("NM: Response=" + string(buf))

	defer a.Stop()
}
