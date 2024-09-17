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

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/zdsapi"
)

type ztunnelUDSConnection struct {
	uuid    uuid.UUID
	u       *net.UnixConn
	updates chan UpdateRequest
}

type updateRequest struct {
	update *zdsapi.WorkloadRequest
	fd     *int

	resp chan updateResponse
}

func (z *ztunnelServer) accept() (ZtunnelConnection, error) {
	ul, ok := z.listener.(*net.UnixListener)
	if !ok {
		return nil, fmt.Errorf("listener is not a unix listener")
	}
	log.Debug("accepting unix conn")
	conn, err := ul.AcceptUnix()
	if err != nil {
		return nil, fmt.Errorf("failed to accept unix: %w", err)
	}
	log.Debug("accepted conn")
	return newZtunnelConnection(conn), nil
}

func (z *ztunnelServer) handleWorkloadInfo(wl WorkloadInfo, uid string, conn ZtunnelConnection) (*zdsapi.WorkloadResponse, error) {
	// We don't need there to be any netns field; we can get namespace guid from the zds WorkloadInfo
	if wl.NetnsCloser() != nil {
		netns := wl.NetnsCloser()
		fd := int(netns.Fd())
		log.Infof("sending pod to ztunnel as part of snapshot")
		return conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Add{
				Add: &zdsapi.AddWorkload{
					Uid:          uid,
					WorkloadInfo: wl.Workload(),
				},
			},
		}, &fd)
	}

	log.Infof("netns is not available for pod, sending 'keep' to ztunnel")
	return conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Keep{
			Keep: &zdsapi.KeepWorkload{
				Uid: uid,
			},
		},
	}, nil)
}

func (ur updateRequest) Update() *zdsapi.WorkloadRequest {
	return ur.update
}

func (ur updateRequest) Fd() *int {
	return ur.fd
}

func (ur updateRequest) Resp() chan updateResponse {
	return ur.resp
}

func newZtunnelConnection(u net.Conn) ZtunnelConnection {
	unixConn := u.(*net.UnixConn)
	return &ztunnelUDSConnection{uuid: uuid.New(), u: unixConn, updates: make(chan UpdateRequest, 100)}
}

func (z *ztunnelUDSConnection) SendDataAndWaitForAck(data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
	var rights []byte
	if fd != nil {
		rights = unix.UnixRights(*fd)
	}
	err := z.u.SetWriteDeadline(time.Now().Add(readWriteDeadline))
	if err != nil {
		return nil, err
	}

	_, _, err = z.u.WriteMsgUnix(data, rights, nil)
	if err != nil {
		return nil, err
	}

	// wait for ack
	return z.readMessage(readWriteDeadline)
}

func (z *ztunnelUDSConnection) readMessage(timeout time.Duration) (*zdsapi.WorkloadResponse, error) {
	m, _, err := readProto[zdsapi.WorkloadResponse](z.u, timeout, nil)
	return m, err
}

func readProto[T any, PT interface {
	proto.Message
	*T
}](c net.Conn, timeout time.Duration, oob []byte) (PT, int, error) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return nil, -1, fmt.Errorf("couldn't convert %q to unixConn", c)
	}
	var buf [1024]byte
	err := c.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, 0, err
	}
	n, oobn, flags, _, err := u.ReadMsgUnix(buf[:], oob)
	if err != nil {
		return nil, 0, err
	}
	if flags&unix.MSG_TRUNC != 0 {
		return nil, 0, fmt.Errorf("truncated message")
	}
	if flags&unix.MSG_CTRUNC != 0 {
		return nil, 0, fmt.Errorf("truncated control message")
	}
	var resp T
	var respPtr PT = &resp
	err = proto.Unmarshal(buf[:n], respPtr)
	if err != nil {
		return nil, 0, err
	}
	return respPtr, oobn, nil
}

func (z ztunnelUDSConnection) Close() {
	z.u.Close()
}

func (z ztunnelUDSConnection) UUID() uuid.UUID {
	return z.uuid
}

func (z ztunnelUDSConnection) Updates() <-chan UpdateRequest {
	return z.updates
}

// do a short read, just to see if the connection to ztunnel is
// still alive. As ztunnel shouldn't send anything unless we send
// something first, we expect to get an os.ErrDeadlineExceeded error
// here if the connection is still alive.
// note that unlike tcp connections, reading is a good enough test here.
func (z ztunnelUDSConnection) CheckAlive(timeout time.Duration) error {
	_, err := z.readMessage(timeout)
	return err
}

func (z ztunnelUDSConnection) ReadHello() (*zdsapi.ZdsHello, error) {
	// get hello message from ztunnel
	m, _, err := readProto[zdsapi.ZdsHello](z.u, readWriteDeadline, nil)
	return m, err
}

func (z ztunnelUDSConnection) Send(ctx context.Context, data *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	ret := make(chan updateResponse, 1)
	req := updateRequest{
		update: data,
		fd:     fd,
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

func (z ztunnelUDSConnection) SendMsgAndWaitForAck(msg *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return z.sendDataAndWaitForAck(data, fd)
}

func (z ztunnelUDSConnection) sendDataAndWaitForAck(data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
	var rights []byte
	if fd != nil {
		rights = unix.UnixRights(*fd)
	}
	err := z.u.SetWriteDeadline(time.Now().Add(readWriteDeadline))
	if err != nil {
		return nil, err
	}

	_, _, err = z.u.WriteMsgUnix(data, rights, nil)
	if err != nil {
		return nil, err
	}

	// wait for ack
	return z.readMessage(readWriteDeadline)
}

func newZtunnelServer(addr string, pods PodNetnsCache, keepaliveInterval time.Duration) (*ztunnelServer, error) {
	if addr == "" {
		return nil, fmt.Errorf("addr cannot be empty")
	}

	resolvedAddr, err := net.ResolveUnixAddr("unixpacket", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve unix addr: %w", err)
	}
	// remove potentially existing address
	// Remove unix socket before use, if one is leftover from previous CNI restart
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		return nil, fmt.Errorf("failed to remove unix://%s: %w", addr, err)
	}

	l, err := net.ListenUnix("unixpacket", resolvedAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen unix: %w", err)
	}

	return &ztunnelServer{
		listener: l,
		conns: &connMgr{
			connectionSet: []ZtunnelConnection{},
		},
		pods:              pods,
		keepaliveInterval: keepaliveInterval,
	}, nil
}

func (z *ztunnelServer) PodAdded(ctx context.Context, pod *v1.Pod, netns Netns) error {
	latestConn, err := z.conns.LatestConn()
	if err != nil {
		return fmt.Errorf("no ztunnel connection")
	}

	uid := string(pod.ObjectMeta.UID)

	add := &zdsapi.AddWorkload{
		WorkloadInfo: podToWorkload(pod),
		Uid:          uid,
	}
	r := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Add{
			Add: add,
		},
	}
	log := log.WithLabels(
		"uid", add.Uid,
		"name", add.WorkloadInfo.Name,
		"namespace", add.WorkloadInfo.Namespace,
		"serviceAccount", add.WorkloadInfo.ServiceAccount,
		"conn_uuid", latestConn.UUID(),
	)

	log.Infof("sending pod add to ztunnel")

	fd := int(netns.Fd())
	resp, err := latestConn.Send(ctx, r, &fd)
	if err != nil {
		return err
	}
	log.Debug("sent pod add to ztunnel")

	if resp.GetAck().GetError() != "" {
		log.Errorf("failed to add workload: %s", resp.GetAck().GetError())
		return fmt.Errorf("got ack error: %s", resp.GetAck().GetError())
	}
	return nil
}
