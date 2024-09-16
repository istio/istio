//go:build windows
// +build windows

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
	"fmt"
	"net"
	"os"
	"time"

	winio "github.com/Microsoft/go-winio"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/zdsapi"
)

type updateRequest struct {
	update []byte
	resp   chan updateResponse
}

type ztunnelConnection struct {
	pc      winio.PipeConn
	updates chan UpdateRequest
}

func newZtunnelConnection(u net.Conn) ZtunnelConnection {
	npConn := u.(winio.PipeConn)
	return &ztunnelConnection{pc: npConn, updates: make(chan UpdateRequest, 100)}
}

func (ur updateRequest) Update() []byte {
	return ur.update
}

func (ur updateRequest) Fd() *int {
	return nil
}

func (ur updateRequest) Resp() chan updateResponse {
	return ur.resp
}

func (z *ztunnelConnection) Conn() net.Conn {
	return z.pc
}

func (z *ztunnelConnection) Updates() chan UpdateRequest {
	return z.updates
}

func (z *ztunnelConnection) Close() {
	z.pc.Close()
}

// The ancillary data isn't used in the windows version of this method
func (z *ztunnelConnection) SendMsgAndWaitForAck(m *zdsapi.WorkloadRequest, _ *int) (*zdsapi.WorkloadResponse, error) {
	data, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}

	return z.SendDataAndWaitForAck(data, nil)
}

func (z *ztunnelConnection) Send(ctx context.Context, data []byte, _ *int) (*zdsapi.WorkloadResponse, error) {
	ret := make(chan updateResponse, 1)
	req := updateRequest{
		update: data,
		resp:   ret,
	}
	select {
	case z.updates <- req:
	case <-ctx.Done():
		return nil, fmt.Errorf("context expired before request sent: %v", ctx.Err())
	}

	select {
	case r := <-ret:
		return r.resp, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("context expired before response received: %v", ctx.Err())
	}
}

func (z *ztunnelConnection) SendDataAndWaitForAck(data []byte, _ *int) (*zdsapi.WorkloadResponse, error) {
	err := z.pc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return nil, err
	}

	n, err := z.pc.Write(data)
	log.Debugf("Sent %d bytes of data to ztunnel client", n)
	if err != nil {
		return nil, err
	}

	return z.ReadMessage(5 * time.Second)
}

func (z *ztunnelConnection) ReadMessage(timeout time.Duration) (*zdsapi.WorkloadResponse, error) {
	m, _, err := readProto[zdsapi.WorkloadResponse](z.pc, timeout, nil)
	return m, err
}

// Need the ignored byte argument to match the linux signature
func readProto[T any, PT interface {
	proto.Message
	*T
}](c net.Conn, timeout time.Duration, _ []byte) (PT, int, error) {
	pc, ok := c.(winio.PipeConn) // Perform type assertion just to confirm
	if !ok {
		return nil, -1, fmt.Errorf("couldn't convert %q to PipeConn", c)
	}
	var buf [1024]byte
	err := pc.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, 0, err
	}
	n, err := pc.Read(buf[:])
	if err != nil {
		return nil, 0, err
	}
	var resp T
	var respPtr PT = &resp
	err = proto.Unmarshal(buf[:n], respPtr)
	if err != nil {
		return nil, 0, err
	}
	return respPtr, n, nil
}

func newZtunnelServer(pipePath string, pods PodNetnsCache) (*ztunnelServer, error) {
	if pipePath == "" {
		return nil, fmt.Errorf("addr cannot be empty")
	}

	// remove potentially existing address
	// Remove unix socket before use, if one is leftover from previous CNI restart
	if err := os.Remove(pipePath); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		return nil, fmt.Errorf("failed to remove %s: %w", pipePath, err)
	}

	pc := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;AU)", // TODO: This is a placeholder, need to revisit
		InputBufferSize:    1024,
		OutputBufferSize:   1024,
		MessageMode:        true,
	}
	l, err := winio.ListenPipe(pipePath, pc)

	if err != nil {
		return nil, fmt.Errorf("failed to listen named pipe: %w", err)
	}

	return &ztunnelServer{
		listener: l,
		conns: &connMgr{
			connectionSet: map[ZtunnelConnection]struct{}{},
		},
		pods: pods,
	}, nil
}

func (z *ztunnelServer) accept() (ZtunnelConnection, error) {
	log.Debug("accepting named pipe conn")
	conn, err := z.listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("failed to accept named pipe conn: %w", err)
	}
	log.Debug("accepted conn")
	return newZtunnelConnection(conn), nil
}

func (z *ztunnelServer) handleWorkloadInfo(wl WorkloadInfo, uid string, conn ZtunnelConnection) (*zdsapi.WorkloadResponse, error) {
	if wl.NetnsCloser() != nil {
		nc, ok := wl.NetnsCloser().(NamespaceCloser)
		if !ok {
			return nil, fmt.Errorf("failed to convert to NamespaceCloser")
		}
		namespace := nc.Namespace()
		log.Infof("sending pod to ztunnel as part of snapshot")
		return conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Add{
				Add: &zdsapi.AddWorkload{
					Uid:                  uid,
					WorkloadInfo:         wl.Workload(),
					WindowsNamespaceGuid: namespace.GUID,
				},
			},
		}, nil)
	}

	log.Infof("namespace info is not available for pod, sending 'keep' to ztunnel")
	return conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Keep{
			Keep: &zdsapi.KeepWorkload{
				Uid: uid,
			},
		},
	}, nil)
}
