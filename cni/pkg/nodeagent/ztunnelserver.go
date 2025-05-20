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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
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
	log.Infof("new ztunnel connected, total connected: %d", len(c.connectionSet))
	ztunnelConnected.RecordInt(int64(len(c.connectionSet)))
}

func (c *connMgr) LatestConn() (ZtunnelConnection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.connectionSet) == 0 {
		return nil, fmt.Errorf("no connection")
	}
	lConn := c.connectionSet[len(c.connectionSet)-1]
	log.Debugf("latest ztunnel connection is %s, total connected: %d", lConn.UUID(), len(c.connectionSet))
	return lConn, nil
}

func (c *connMgr) deleteConn(conn ZtunnelConnection) {
	log.Debug("ztunnel disconnected")
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
	log.Infof("ztunnel disconnected, total connected %d", len(c.connectionSet))
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
	listener net.Listener

	// connections to pod delivered map
	// add pod goes to newest connection
	// delete pod goes to all connections
	conns             *connMgr
	pods              PodNetnsCache
	keepaliveInterval time.Duration
}

var _ ZtunnelServer = &ztunnelServer{}

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
	log.Infof("sending snapshot to ztunnel")
	if err := z.sendSnapshot(ctx, conn); err != nil {
		return err
	}
	for {
		// listen for updates:
		select {
		case update, ok := <-conn.Updates():
			if !ok {
				log.Infof("update channel closed - returning")
				return nil
			}
			log.Debugf("got update to send to ztunnel")
			resp, err := conn.SendMsgAndWaitForAck(update.Update(), update.Fd())
			if err != nil {
				// Two possibilities
				// - we couldn't _write_ to the connection (in which case, this conn is dead)
				// (annoyingly, go's `net.OpErr` is not convertible?)
				if strings.Contains(err.Error(), "sendmsg: broken pipe") {
					log.Error("ztunnel connection broken/unwritable, disposing of this connection")
					update.Resp() <- updateResponse{
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
			log.Infof("ztunnel acked")
			// Safety: Resp is buffered, so this will not block
			update.Resp() <- updateResponse{
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
			case !errors.Is(err, z.timeoutError()):
				log.Infof("ztunnel keepalive failed: %v", err)
				if errors.Is(err, io.EOF) {
					log.Info("ztunnel EOF")
					return nil
				}
				return err
			case err == nil:
				log.Info("ztunnel protocol error, unexpected message")
				return fmt.Errorf("ztunnel protocol error, unexpected message")
			default:
				// we get here if error is deadline exceeded, which means ztunnel is alive.
			}

		case <-ctx.Done():
			return nil
		}
	}
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

func (z *ztunnelServer) sendSnapshot(_ context.Context, conn ZtunnelConnection) error {
	snap := z.pods.ReadCurrentPodSnapshot()
	for uid, wl := range snap {
		var resp *zdsapi.WorkloadResponse
		var err error
		log := log.WithLabels("uid", uid)
		if wl.Workload() != nil {
			log = log.WithLabels(
				"name", wl.Workload().Name,
				"namespace", wl.Workload().Namespace,
				"serviceAccount", wl.Workload().ServiceAccount,
				"windowsNamespaceGuid", wl.Workload().WindowsNamespace)
		}
		resp, err = z.handleWorkloadInfo(wl, uid, conn)
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
	log.Infof("snapshot sent to ztunnel")
	if resp.GetAck().GetError() != "" {
		log.Errorf("snap-sent: got ack error: %s", resp.GetAck().GetError())
	}

	return nil
}

type updateResponse struct {
	err  error
	resp *zdsapi.WorkloadResponse
}

type UpdateRequest interface {
	Update() *zdsapi.WorkloadRequest
	Fd() *int

	Resp() chan updateResponse
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
