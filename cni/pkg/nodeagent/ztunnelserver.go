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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/zdsapi"
)

var readWriteDeadline = 5 * time.Second

var ztunnelConnected = monitoring.NewGauge("ztunnel_connected",
	"number of connections to ztunnel")

type ZtunnelServer interface {
	Run(ctx context.Context)
	PodDeleted(ctx context.Context, uid string) error
	PodAdded(ctx context.Context, pod *v1.Pod, netns Netns) error
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
	connectionSet []ZtunnelConnection
	mu            sync.Mutex
}

func (c *connMgr) addConn(conn ZtunnelConnection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	log := log.WithLabels("conn_uuid", conn.UUID())
	c.connectionSet = append(c.connectionSet, conn)
	log.Infof("new ztunnel connected, total connected: %v", len(c.connectionSet))
	ztunnelConnected.RecordInt(int64(len(c.connectionSet)))
}

func (c *connMgr) LatestConn() (ZtunnelConnection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.connectionSet) == 0 {
		return nil, fmt.Errorf("no connection")
	}
	lConn := c.connectionSet[len(c.connectionSet)-1]
	log.Debugf("latest ztunnel connection is %s, total connected: %v", lConn.UUID(), len(c.connectionSet))
	return lConn, nil
}

func (c *connMgr) deleteConn(conn ZtunnelConnection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	log := log.WithLabels("conn_uuid", conn.UUID())

	// Loop over the slice, keeping non-deleted conn but
	// filtering out the deleted one.
	var retainedConns []ZtunnelConnection
	for _, existingConn := range c.connectionSet {
		// Not conn that was deleted? Keep it.
		if existingConn != conn {
			retainedConns = append(retainedConns, existingConn)
		}
	}
	c.connectionSet = retainedConns
	log.Infof("ztunnel disconnected, total connected %s", len(c.connectionSet))
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
	conns             *connMgr
	pods              PodNetnsCache
	keepaliveInterval time.Duration
}

var _ ZtunnelServer = &ztunnelServer{}

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

func (z *ztunnelServer) Close() error {
	return z.listener.Close()
}

func (z *ztunnelServer) Run(ctx context.Context) {
	context.AfterFunc(ctx, func() { _ = z.Close() })

	// Allow at most 5 requests per second. This is still a ridiculous amount; at most we should have 2 ztunnels on our node,
	// and they will only connect once and persist.
	// However, if they do get in a state where they call us in a loop, we will quickly OOM
	limit := rate.NewLimiter(rate.Limit(5), 1)
	for {
		log.Debug("accepting conn")
		if err := limit.Wait(ctx); err != nil {
			log.Errorf("failed to wait for ztunnel connection: %v", err)
			return
		}
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
			log := log.WithLabels("conn_uuid", conn.UUID())
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
func (z *ztunnelServer) handleConn(ctx context.Context, conn ZtunnelConnection) error {
	defer conn.Close()

	// before doing anything, add the connection to the list of active connections
	z.conns.addConn(conn)
	defer z.conns.deleteConn(conn)

	log := log.WithLabels("conn_uuid", conn.UUID())

	m, err := conn.ReadHello()
	if err != nil {
		return err
	}

	log.WithLabels("version", m.Version).Infof("received hello from ztunnel")
	log.Debug("sending snapshot to ztunnel")
	if err := z.sendSnapshot(ctx, conn); err != nil {
		return err
	}
	for {
		// listen for updates:
		select {
		case update, ok := <-conn.Updates():
			if !ok {
				log.Debug("update channel closed - returning")
				return nil
			}
			log.Debugf("got update to send to ztunnel")
			resp, err := conn.SendMsgAndWaitForAck(update.Update, update.Fd)
			if err != nil {
				// Two possibilities
				// - we couldn't _write_ to the connection (in which case, this conn is dead)
				// (annoyingly, go's `net.OpErr` is not convertible?)
				if strings.Contains(err.Error(), "sendmsg: broken pipe") {
					log.Error("ztunnel connection broken/unwritable, disposing of this connection")
					update.Resp <- updateResponse{
						err:  err,
						resp: nil,
					}
					return err
				}
				// if we timed out waiting for a (valid) response, mention and continue, connection may not be trashed
				log.Warnf("timed out waiting for valid ztunnel response: %s", err)

				if resp.GetAck().GetError() != "" {
					// - we wrote, got a response, but ztunnel responded with an `ack` error (in which case, this conn is not dead)
					log.Errorf("ztunnel responded with an ack error: ackErr %s", resp.GetAck().GetError())
				}
			}
			log.Debugf("ztunnel acked")
			// Safety: Resp is buffered, so this will not block
			update.Resp <- updateResponse{
				err:  err,
				resp: resp,
			}

		case <-time.After(z.keepaliveInterval):
			// do a short read, just to see if the connection to ztunnel is
			// still alive. As ztunnel shouldn't send anything unless we send
			// something first, we expect to get an os.ErrDeadlineExceeded error
			// here if the connection is still alive.
			// note that unlike tcp connections, reading is a good enough test here.
			err := conn.CheckAlive(time.Second / 100)
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

// PodDeleted sends a pod deletion notification to connected ztunnels.
//
// Note that unlike PodAdded, this deletion event is broadcast to *all*
// currently-connected ztunnels - not just the latest.
// This is intentional, and critical to handle proper shutdown/reconnect
// cycles.
func (z *ztunnelServer) PodDeleted(ctx context.Context, uid string) error {
	r := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Del{
			Del: &zdsapi.DelWorkload{
				Uid: uid,
			},
		},
	}

	log.Debugf("sending delete pod to all ztunnels: %s %v", uid, r)

	var delErr []error

	z.conns.mu.Lock()
	defer z.conns.mu.Unlock()
	for _, conn := range z.conns.connectionSet {
		log := log.WithLabels("conn_uuid", conn.UUID())
		log.Debug("sending msg to connected ztunnel")
		_, err := conn.Send(ctx, r, nil)
		if err != nil {
			delErr = append(delErr, err)
		}
	}
	return errors.Join(delErr...)
}

func podToWorkload(pod *v1.Pod) *zdsapi.WorkloadInfo {
	namespace := pod.ObjectMeta.Namespace
	name := pod.ObjectMeta.Name
	svcAccount := pod.Spec.ServiceAccountName
	return &zdsapi.WorkloadInfo{
		Namespace:      namespace,
		Name:           name,
		ServiceAccount: svcAccount,
	}
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

// TODO ctx is unused here
// nolint: unparam
func (z *ztunnelServer) sendSnapshot(ctx context.Context, conn ZtunnelConnection) error {
	snap := z.pods.ReadCurrentPodSnapshot()
	for uid, wl := range snap {
		var resp *zdsapi.WorkloadResponse
		var err error
		log := log.WithLabels("uid", uid)
		if wl.Workload != nil {
			log = log.WithLabels(
				"name", wl.Workload.Name,
				"namespace", wl.Workload.Namespace,
				"serviceAccount", wl.Workload.ServiceAccount)
		}
		if wl.Netns != nil {
			fd := int(wl.Netns.Fd())
			log.Infof("sending pod to ztunnel as part of snapshot")
			resp, err = conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
				Payload: &zdsapi.WorkloadRequest_Add{
					Add: &zdsapi.AddWorkload{
						Uid:          uid,
						WorkloadInfo: wl.Workload,
					},
				},
			}, &fd)
		} else {
			log.Infof("netns is not available for pod, sending 'keep' to ztunnel")
			resp, err = conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
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
	resp, err := conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_SnapshotSent{
			SnapshotSent: &zdsapi.SnapshotSent{},
		},
	}, nil)
	if err != nil {
		return err
	}
	log.Debugf("snapshot sent to ztunnel")
	if resp.GetAck().GetError() != "" {
		log.Errorf("snap-sent: got ack error: %s", resp.GetAck().GetError())
	}

	return nil
}

func (z *ztunnelServer) accept() (ZtunnelConnection, error) {
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

type UpdateRequest struct {
	Update *zdsapi.WorkloadRequest
	Fd     *int

	Resp chan updateResponse
}

type ZtunnelConnection interface {
	Close()
	UUID() uuid.UUID
	Updates() <-chan UpdateRequest
	CheckAlive(timeout time.Duration) error
	ReadHello() (*zdsapi.ZdsHello, error)
	Send(ctx context.Context, data *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error)
	SendMsgAndWaitForAck(msg *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error)
}

type ZtunnelUDSConnection struct {
	uuid    uuid.UUID
	u       *net.UnixConn
	updates chan UpdateRequest
}

func newZtunnelConnection(u *net.UnixConn) ZtunnelConnection {
	return ZtunnelUDSConnection{uuid: uuid.New(), u: u, updates: make(chan UpdateRequest, 100)}
}

func (z ZtunnelUDSConnection) Close() {
	z.u.Close()
}

func (z ZtunnelUDSConnection) UUID() uuid.UUID {
	return z.uuid
}

func (z ZtunnelUDSConnection) Updates() <-chan UpdateRequest {
	return z.updates
}

// do a short read, just to see if the connection to ztunnel is
// still alive. As ztunnel shouldn't send anything unless we send
// something first, we expect to get an os.ErrDeadlineExceeded error
// here if the connection is still alive.
// note that unlike tcp connections, reading is a good enough test here.
func (z ZtunnelUDSConnection) CheckAlive(timeout time.Duration) error {
	_, err := z.readMessage(timeout)
	return err
}

func (z ZtunnelUDSConnection) ReadHello() (*zdsapi.ZdsHello, error) {
	// get hello message from ztunnel
	m, _, err := readProto[zdsapi.ZdsHello](z.u, readWriteDeadline, nil)
	return m, err
}

func (z ZtunnelUDSConnection) Send(ctx context.Context, data *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	ret := make(chan updateResponse, 1)
	req := UpdateRequest{
		Update: data,
		Fd:     fd,
		Resp:   ret,
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

func (z ZtunnelUDSConnection) SendMsgAndWaitForAck(msg *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return z.sendDataAndWaitForAck(data, fd)
}

func (z ZtunnelUDSConnection) sendDataAndWaitForAck(data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
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

func (z ZtunnelUDSConnection) readMessage(timeout time.Duration) (*zdsapi.WorkloadResponse, error) {
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
