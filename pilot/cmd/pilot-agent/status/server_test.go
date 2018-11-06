// Copyright 2018 Istio Authors
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

package status

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo"
)

var (
	appPort uint16
)

func init() {
	appFactory := &echo.Factory{
		Ports: model.PortList{{
			Name:     "http",
			Protocol: model.ProtocolHTTP,
		}},
		Version: "version-foo",
	}
	app, err := appFactory.NewApplication(application.Dialer{HTTP: application.DefaultHTTPDoFunc})
	if err != nil {
		log.Fatalf("Failed to create application %v", err)
	}
	log.Fatalf("application created %v", app.GetPorts())
	appPort = uint16(app.GetPorts()[0].Port)
}

func TestAppProbe(t *testing.T) {
	server := NewServer(Config{
		StatusPort:      0,
	})
	go server.Run(context.Background())

	// We wait a bit here to ensure server's statusPort is updated.
	time.Sleep(time.Second * 3)

	server.mutex.RLock()
	statusPort := server.statusPort
	server.mutex.RUnlock()
	t.Logf("status server starts at port %v, app starts at port %v", statusPort, appPort)
	testCases := []struct {
		probePath  string
		appPortHeader string
		statusCode int
		err        string
	}{
		{
			probePath:  fmt.Sprintf(":%v/", statusPort),
			appPortHeader: fmt.Sprintf("%v", appPort),
			statusCode: 200,
		},
		{
			probePath:  fmt.Sprintf(":%v/ill-formed-path", statusPort),
			statusCode: 400,
		},
	}
	for _, tc := range testCases {
		client := http.Client{}
		req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s", tc.probePath), nil)
		if err != nil {
			t.Errorf("[%v] failed to create request", tc.probePath)
		}
		if tc.appPortHeader != "" {
			req.Header.Add(IstioAppPortHeader, tc.appPortHeader)
		}
		resp, _ := client.Do(req)
		if resp.StatusCode != tc.statusCode {
			t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
		}
	}
}
