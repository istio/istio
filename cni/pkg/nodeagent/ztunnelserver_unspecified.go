//go:build !linux && !windows
// +build !linux,!windows

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nodeagent

import (
	"context"
	"errors"

	"istio.io/istio/pkg/zdsapi"
	v1 "k8s.io/api/core/v1"
)

var errNotImplemented = errors.New("not implemented on this platform")

func (z *ztunnelServer) PodAdded(ctx context.Context, pod *v1.Pod, netns Netns) error {
	return errNotImplemented
}

func (z *ztunnelServer) accept() (ZtunnelConnection, error) {
	return nil, errNotImplemented
}

func (z *ztunnelServer) timeoutError() error {
	return errNotImplemented
}

func (z *ztunnelServer) handleWorkloadInfo(wl WorkloadInfo, uid string, conn ZtunnelConnection) (*zdsapi.WorkloadResponse, error) {
	return nil, errNotImplemented
}
