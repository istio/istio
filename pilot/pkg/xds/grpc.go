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

package xds

import (
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pilot/pkg/model"
)

type (
	SendFunc  func(errorChan chan error)
	ErrorFunc func()
)

// Send with timeout
func Send(res *discovery.DiscoveryResponse) error {
	errChan := make(chan error, 1)

	// sendTimeout may be modified via environment
	t := time.NewTimer(sendTimeout)
	go func() {
		start := time.Now()
		defer func() { recordSendTime(time.Since(start)) }()
		errChan <- conn.stream.Send(res)
		close(errChan)
	}()

	select {
	case <-t.C:
		log.Infof("Timeout writing %s", conn.ConID)
		xdsResponseWriteTimeouts.Increment()
		return status.Errorf(codes.DeadlineExceeded, "timeout sending")
	case err := <-errChan:
		if err == nil {
			sz := 0
			for _, rc := range res.Resources {
				sz += len(rc.Value)
			}
			conn.proxy.Lock()
			if res.Nonce != "" {
				if conn.proxy.WatchedResources[res.TypeUrl] == nil {
					conn.proxy.WatchedResources[res.TypeUrl] = &model.WatchedResource{TypeUrl: res.TypeUrl}
				}
				conn.proxy.WatchedResources[res.TypeUrl].NonceSent = res.Nonce
				conn.proxy.WatchedResources[res.TypeUrl].VersionSent = res.VersionInfo
				conn.proxy.WatchedResources[res.TypeUrl].LastSent = time.Now()
				conn.proxy.WatchedResources[res.TypeUrl].LastSize = sz
			}
			conn.proxy.Unlock()
		}
		// To ensure the channel is empty after a call to Stop, check the
		// return value and drain the channel (from Stop docs).
		if !t.Stop() {
			<-t.C
		}
		return err
	}
}
