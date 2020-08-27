package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/istio/pkg/test/echo/server/forwarder"
	"istio.io/pkg/log"
)

const (
	version = "v2"
	cluster = "cluster-1"
	msg = "Hello world!"
)

var testCases = map[string]struct {
	proto       protocol.Instance
	serverFirst bool
}{
	"tcp": {
		proto: protocol.TCP,
	},
}

func TestEcho(t *testing.T) {
	for _, s := range log.Scopes() {
		s.SetOutputLevel(log.WarnLevel)
	}
	dialer := common.Dialer{}.FillInDefaults()
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
				t.Error(err)
			}
			if err := parsedRes.CheckCluster(cluster); err != nil {
				t.Error(err)
			}
			if err := parsedRes.Check(func(i int, response *client.ParsedResponse) error {
				if response.Count(fmt.Sprintf("body] %s", msg)) != 1 {
					return fmt.Errorf("did not find %q in res %d", msg, i)
				}
				return nil
			}); err != nil {
				t.Error(err)
			}

		})
	}
}
