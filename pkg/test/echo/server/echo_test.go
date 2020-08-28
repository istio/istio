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

package server

import (
	"context"
	"fmt"
	"istio.io/pkg/log"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/istio/pkg/test/echo/server/forwarder"
)

const (
	version = "v2"
	cluster = "cluster-1"
	msg     = "Hello world!"
)

var testCases = map[string]struct {
	proto       protocol.Instance
	serverFirst bool
}{
	"tcp": {
		proto: protocol.TCP,
	},
	//"tcp server-first": {
	//	proto:       protocol.TCP,
	//	serverFirst: true,
	//},
	//"http": {
	//	proto:       protocol.HTTP,
	//},
}

func TestEcho(t *testing.T) {
	dialer := common.Dialer{}.FillInDefaults()
	for _, s := range log.Scopes() {
		s.SetOutputLevel(log.NoneLevel)
	}
	for i := 0; i < 100; i++ {
		fmt.Println(i)
		for name, tt := range testCases {
			tt := tt
			t.Run(name, func(t *testing.T) {
				ep, err := endpoint.New(endpoint.Config{
					IsServerReady: func() bool { return true },
					Version:       version,
					Cluster:       cluster,
					Dialer:        dialer,
					Port: &common.Port{
						Port:        7070,
						Protocol:    tt.proto,
						ServerFirst: tt.serverFirst,
					},
				})
				if err != nil {
					t.Fatal(err)
				}

				readyChan := make(chan struct{}, 1)
				if err := ep.Start(func() {
					readyChan <- struct{}{}
				}); err != nil {
					t.Fatal(err)
				}
				<-readyChan
				defer func() { _ = ep.Close() }()

				fw, err := forwarder.New(forwarder.Config{
					Request: &proto.ForwardEchoRequest{
						Count:         100,
						TimeoutMicros: common.DurationToMicros(5 * time.Second),
						Url:           fmt.Sprintf("%s://127.0.0.1:%d", tt.proto, ep.GetConfig().Port.Port),
						ServerFirst:   tt.serverFirst,
						Message:       msg,
					},
					Dialer: dialer,
				})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = fw.Close() }()

				res, err := fw.Run(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				parsedRes := client.ParseForwardedResponse(res)
				if err := parsedRes.CheckOK(); err != nil {
					t.Fatal(err)
				}
				if err := parsedRes.CheckCluster(cluster); err != nil {
					t.Fatal(err)
				}
			})
			if t.Failed() {
				t.FailNow()
			}
		}
	}
}
