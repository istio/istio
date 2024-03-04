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

package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/zdsapi"
)

var (
	ztunnelKeepAliveCheckInterval = 5 * time.Second
	readWriteDeadline             = 5 * time.Second
)

var ztunnelConnected = monitoring.NewGauge("ztunnel_connected",
	"number of connections to ztunnel")

type ZtunnelServer interface {
	Run(ctx context.Context)
	PodDeleted(ctx context.Context, uid string) error
	PodAdded(ctx context.Context, uid string, netns Netns) error
	Close() error
}

/*
To clean up stale ztunnels

	we may need to ztunnel to send its (uid, bootid / boot time) to us
	so that we can remove stale entries when the ztunnel pod is deleted
	or when the ztunnel pod is restarted in the same pod (remove old entries when the same uid connects again, but with different boot id?)

	save a queue of what needs to be sent to the ztunnel pod and send it one by one when it connects.

	when a new ztunnel connects with different uid, only propagate deletes to older ztunnels.
*/

type connMgr struct {
	connectionSet map[*ZtunnelConnection]struct{}
	latestConn    *ZtunnelConnection
	mu            sync.Mutex
}

func (c *connMgr) addConn(conn *ZtunnelConnection) {
	log.Debug("ztunnel connected")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectionSet[conn] = struct{}{}
	c.latestConn = conn
	ztunnelConnected.RecordInt(int64(len(c.connectionSet)))
}

func (c *connMgr) LatestConn() *ZtunnelConnection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latestConn
}

func (c *connMgr) deleteConn(conn *ZtunnelConnection) {
	log.Debug("ztunnel disconnected")
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.connectionSet, conn)
	if c.latestConn == conn {
		c.latestConn = nil
	}
	ztunnelConnected.RecordInt(int64(len(c.connectionSet)))
}

// this is used in tests
// nolint: unused
func (c *connMgr) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.connectionSet)
}

type ztunnelServer struct {
	listener *net.UnixListener

	// connections to pod delivered map
	// add pod goes to newest connection
	// delete pod goes to all connections
	conns *connMgr
	pods  PodNetnsCache
}

var _ ZtunnelServer = &ztunnelServer{}

func newZtunnelServer(addr string, pods PodNetnsCache) (*ztunnelServer, error) {
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
			connectionSet: map[*ZtunnelConnection]struct{}{},
		},
		pods: pods,
	}, nil
}

func (z *ztunnelServer) Close() error {
	return z.listener.Close()
}

func (z *ztunnelServer) Run(ctx context.Context) {
	context.AfterFunc(ctx, func() { _ = z.Close() })

	for {
		log.Debug("accepting conn")
		conn, err := z.accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Debug("listener closed - returning")
				return
			}

			log.Errorf("failed to accept conn: %v", err)
			continue
		}
		log.Debug("connection accepted")
		go func() {
			log.Debug("handling conn")
			if err := z.handleConn(ctx, conn); err != nil {
				log.Errorf("failed to handle conn: %v", err)
			}
		}()
	}
}

// ZDS protocol is very simple, for every message sent, and ack is sent.
// the ack only has temporal correlation (i.e. it is the first and only ack msg after the message was sent)
// All this to say, that we want to make sure that message to ztunnel are sent from a single goroutine
// so we don't mix messages and acks.
// nolint: unparam
func (z *ztunnelServer) handleConn(ctx context.Context, conn *ZtunnelConnection) error {
	defer conn.Close()

	context.AfterFunc(ctx, func() {
		log.Debug("context cancelled - closing conn")
		conn.Close()
	})

	// before doing anything, add the connection to the list of active connections
	z.conns.addConn(conn)
	defer z.conns.deleteConn(conn)

	// get hello message from ztunnel
	m, _, err := readProto[zdsapi.ZdsHello](conn.u, readWriteDeadline, nil)
	if err != nil {
		return err
	}
	log.Infof("received hello from ztunnel. %v", m.Version)
	log.Debug("sending snapshot to ztunnel")
	if err := z.sendSnapshot(ctx, conn); err != nil {
		return err
	}
	for {
		// listen for updates:
		select {
		case update, ok := <-conn.Updates:
			if !ok {
				log.Debug("update channel closed - returning")
				return nil
			}
			log.Debugf("got update to send to ztunnel")
			resp, err := conn.sendDataAndWaitForAck(update.Update, update.Fd)
			if err != nil {
				log.Errorf("ztunnel acked error: err %v ackErr %s", err, resp.GetAck().GetError())
			}
			log.Debugf("ztunnel acked")
			// Safety: Resp is buffered, so this will not block
			update.Resp <- updateResponse{
				err:  err,
				resp: resp,
			}

		case <-time.After(ztunnelKeepAliveCheckInterval):
			// do a short read, just to see if the connection to ztunnel is
			// still alive. As ztunnel shouldn't send anything unless we send
			// something first, we expect to get an os.ErrDeadlineExceeded error
			// here if the connection is still alive.
			// note that unlike tcp connections, reading is a good enough test here.
			_, err := conn.readMessage(time.Second / 100)
			switch {
			case !errors.Is(err, os.ErrDeadlineExceeded):
				log.Debugf("ztunnel keepalive failed: %v", err)
				if errors.Is(err, io.EOF) {
					log.Debug("ztunnel EOF")
					return nil
				}
				return err
			case err == nil:
				log.Warn("ztunnel protocol error, unexpected message")
				return fmt.Errorf("ztunnel protocol error, unexpected message")
			default:
				// we get here if error is deadline exceeded, which means ztunnel is alive.
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (z *ztunnelServer) PodDeleted(ctx context.Context, uid string) error {
	r := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Del{
			Del: &zdsapi.DelWorkload{
				Uid: uid,
			},
		},
	}
	data, err := proto.Marshal(r)
	if err != nil {
		return err
	}

	log.Debugf("sending delete pod to ztunnel: %s %v", uid, r)

	var delErr []error

	z.conns.mu.Lock()
	defer z.conns.mu.Unlock()
	for conn := range z.conns.connectionSet {
		_, err := conn.send(ctx, data, nil)
		if err != nil {
			delErr = append(delErr, err)
		}
	}
	return errors.Join(delErr...)
}

func (z *ztunnelServer) PodAdded(ctx context.Context, uid string, netns Netns) error {
	latestConn := z.conns.LatestConn()
	if latestConn == nil {
		return fmt.Errorf("no ztunnel connection")
	}
	r := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Add{
			Add: &zdsapi.AddWorkload{
				Uid: uid,
			},
		},
	}
	log.Debugf("About to send added pod: %s to ztunnel: %v", uid, r)
	data, err := proto.Marshal(r)
	if err != nil {
		return err
	}

	fd := int(netns.Fd())
	resp, err := latestConn.send(ctx, data, &fd)
	if err != nil {
		return err
	}

	if resp.GetAck().GetError() != "" {
		log.Errorf("add-workload: got ack error: %s", resp.GetAck().GetError())
		return fmt.Errorf("got ack error: %s", resp.GetAck().GetError())
	}
	return nil
}

// TODO ctx is unused here
// nolint: unparam
func (z *ztunnelServer) sendSnapshot(ctx context.Context, conn *ZtunnelConnection) error {
	snap := z.pods.ReadCurrentPodSnapshot()
	for uid, netns := range snap {
		var resp *zdsapi.WorkloadResponse
		var err error
		if netns != nil {
			fd := int(netns.Fd())
			log.Debugf("Sending local pod %s ztunnel", uid)
			resp, err = conn.sendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
				Payload: &zdsapi.WorkloadRequest_Add{
					Add: &zdsapi.AddWorkload{
						Uid: uid,
					},
				},
			}, &fd)
		} else {
			log.Infof("netns not available for local pod %s. sending keep to ztunnel", uid)
			resp, err = conn.sendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
				Payload: &zdsapi.WorkloadRequest_Keep{
					Keep: &zdsapi.KeepWorkload{
						Uid: uid,
					},
				},
			}, nil)
		}
		if err != nil {
			return err
		}
		if resp.GetAck().GetError() != "" {
			log.Errorf("add-workload: got ack error: %s", resp.GetAck().GetError())
		}
	}
	resp, err := conn.sendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_SnapshotSent{
			SnapshotSent: &zdsapi.SnapshotSent{},
		},
	}, nil)
	if err != nil {
		return err
	}
	log.Debugf("snaptshot sent to ztunnel")
	if resp.GetAck().GetError() != "" {
		log.Errorf("snap-sent: got ack error: %s", resp.GetAck().GetError())
	}

	return nil
}

func (z *ztunnelServer) accept() (*ZtunnelConnection, error) {
	log.Debug("accepting unix conn")
	conn, err := z.listener.AcceptUnix()
	if err != nil {
		return nil, fmt.Errorf("failed to accept unix: %w", err)
	}
	log.Debug("accepted conn")
	return newZtunnelConnection(conn), nil
}

type updateResponse struct {
	err  error
	resp *zdsapi.WorkloadResponse
}

type updateRequest struct {
	Update []byte
	Fd     *int

	Resp chan updateResponse
}

type ZtunnelConnection struct {
	u       *net.UnixConn
	Updates chan updateRequest
}

func newZtunnelConnection(u *net.UnixConn) *ZtunnelConnection {
	return &ZtunnelConnection{u: u, Updates: make(chan updateRequest, 100)}
}

func (z *ZtunnelConnection) Close() {
	z.u.Close()
}

func (z *ZtunnelConnection) send(ctx context.Context, data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
	ret := make(chan updateResponse, 1)
	req := updateRequest{
		Update: data,
		Fd:     fd,
		Resp:   ret,
	}
	select {
	case z.Updates <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case r := <-ret:
		return r.resp, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (z *ZtunnelConnection) sendMsgAndWaitForAck(msg *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return z.sendDataAndWaitForAck(data, fd)
}

func (z *ZtunnelConnection) sendDataAndWaitForAck(data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
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

func (z *ZtunnelConnection) readMessage(timeout time.Duration) (*zdsapi.WorkloadResponse, error) {
	m, _, err := readProto[zdsapi.WorkloadResponse](z.u, timeout, nil)
	return m, err
}

func readProto[T any, PT interface {
	proto.Message
	*T
}](c *net.UnixConn, timeout time.Duration, oob []byte) (PT, int, error) {
	var buf [1024]byte
	err := c.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, 0, err
	}
	n, oobn, flags, _, err := c.ReadMsgUnix(buf[:], oob)
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
