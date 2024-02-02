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
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/zdsapi"
)

var ztunnelTestCounter atomic.Uint32

func TestZtunnelSendsPodSnapshot(t *testing.T) {
	ztunnelKeepAliveCheckInterval = time.Second / 10
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fixture := connect(ctx)
	ztunClient := fixture.ztunClient
	uid := fixture.uid

	m, fds := readRequest(t, ztunClient)
	// we got am essage from ztun, so it should have observed us being connected
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))

	// we should get the fd to dev null. note that we can't assert the fd number
	// as the kernel may have given us a different number that refers to the same file.
	assert.Equal(t, len(fds), 1)
	// in theory we should close the fd, but it's just a test..

	assert.Equal(t, m.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid, uid)
	// send ack so the server doesn't wait for us.
	sendAck(ztunClient)

	// second message should be the snap sent message
	m, fds = readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)

	sent := m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(ztunClient)
	ztunClient.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestZtunnelRemovePod(t *testing.T) {
	ztunnelKeepAliveCheckInterval = time.Second / 10
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fixture := connect(ctx)
	ztunClient := fixture.ztunClient
	uid := fixture.uid
	// read initial pod add
	readRequest(t, ztunClient)
	sendAck(ztunClient)
	// read snapshot sent
	m, fds := readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)
	sent := m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(ztunClient)

	// now remove the pod
	ztunnelServer := fixture.ztunServer
	errChan := make(chan error)
	go func() {
		errChan <- ztunnelServer.PodDeleted(ctx, uid)
	}()
	// read the msg to delete from ztunnel
	m, fds = readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)
	assert.Equal(t, m.Payload.(*zdsapi.WorkloadRequest_Del).Del.Uid, uid)
	sendAck(ztunClient)

	assert.NoError(t, <-errChan)

	ztunClient.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestZtunnelPodAdded(t *testing.T) {
	ztunnelKeepAliveCheckInterval = time.Second / 10
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fixture := connect(ctx)
	ztunClient := fixture.ztunClient
	// read initial pod add
	readRequest(t, ztunClient)
	sendAck(ztunClient)
	// read snapshot sent
	m, fds := readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)
	sent := m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(ztunClient)

	// now remove the pod
	ztunnelServer := fixture.ztunServer
	errChan := make(chan error)
	uid2, ns2 := podAndNetns()
	go func() {
		errChan <- ztunnelServer.PodAdded(ctx, uid2, ns2)
	}()
	// read the msg to delete from ztunnel
	m, fds = readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 1)
	assert.Equal(t, m.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid, uid2)
	sendAck(ztunClient)

	assert.NoError(t, <-errChan)

	ztunClient.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestZtunnelPodKept(t *testing.T) {
	ztunnelKeepAliveCheckInterval = time.Second / 10
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pods := &fakePodCache{}

	uid, f := podAndNetns()
	f.Close()
	pods.pods = map[string]Netns{
		uid: nil, // simulate unknown netns
	}

	fixture := connectWithPods(ctx, pods)
	ztunClient := fixture.ztunClient
	// read initial pod add
	keep, fds := readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)
	kept := keep.Payload.(*zdsapi.WorkloadRequest_Keep).Keep
	if kept.Uid != uid {
		panic("expected keep received")
	}

	sendAck(ztunClient)
	// read snapshot sent
	m, fds := readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)
	sent := m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(ztunClient)

	ztunClient.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func podAndNetns() (string, *fakeNs) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		panic(err)
	}
	// we can't close this now, because we need to pass it from the ztunnel server to the client
	// it would leak, but this is a test, so we don't care
	//	defer devNull.Close()

	return fmt.Sprintf("uid%d", ztunnelTestCounter.Add(1)), newFakeNs(devNull.Fd())
}

func connect(ctx context.Context) struct {
	ztunClient *net.UnixConn
	ztunServer *ztunnelServer
	uid        string
} {
	pods := &fakePodCache{}

	uid, ns := podAndNetns()
	pods.pods = map[string]Netns{
		uid: ns,
	}
	ret := connectWithPods(ctx, pods)

	return struct {
		ztunClient *net.UnixConn
		ztunServer *ztunnelServer
		uid        string
	}{ztunClient: ret.ztunClient, ztunServer: ret.ztunServer, uid: uid}
}

func connectWithPods(ctx context.Context, pods PodNetnsCache) struct {
	ztunClient *net.UnixConn
	ztunServer *ztunnelServer
} {
	// go uses @ instead of \0 for abstract unix sockets
	addr := fmt.Sprintf("@testaddr%d", ztunnelTestCounter.Add(1))
	ztun, err := newZtunnelServer(addr, pods)
	if err != nil {
		panic(err)
	}
	go ztun.Run(ctx)

	// now as a client connect confirm we and get snapshot
	resolvedAddr, err := net.ResolveUnixAddr("unixpacket", addr)
	if err != nil {
		panic(err)
	}
	ztunClient, err := net.DialUnix("unixpacket", nil, resolvedAddr)
	if err != nil {
		panic(err)
	}

	// send hello
	sendHello(ztunClient)

	return struct {
		ztunClient *net.UnixConn
		ztunServer *ztunnelServer
	}{ztunClient: ztunClient, ztunServer: ztun}
}

func readRequest(t *testing.T, c *net.UnixConn) (*zdsapi.WorkloadRequest, []int) {
	var oob [1024]byte
	m, oobn, err := readProto[zdsapi.WorkloadRequest](c, time.Second, oob[:])
	if err != nil {
		panic(err)
	}

	receivedoob := oob[:oobn]
	msgs, err := unix.ParseSocketControlMessage(receivedoob)
	if err != nil {
		panic(err)
	}

	// we should get 0 or 1 oob messages
	if len(msgs) != 0 {
		assert.Equal(t, len(msgs), 1)
	}

	var fdss []int
	for _, msg := range msgs {
		fds, err := unix.ParseUnixRights(&msg)
		if err != nil {
			panic(err)
		}
		fdss = append(fdss, fds...)
	}
	return m, fdss
}

func sendAck(c *net.UnixConn) {
	ack := &zdsapi.WorkloadResponse{
		Payload: &zdsapi.WorkloadResponse_Ack{
			Ack: &zdsapi.Ack{},
		},
	}
	data, err := proto.Marshal(ack)
	if err != nil {
		panic(err)
	}
	err = c.SetWriteDeadline(time.Now().Add(time.Second))
	if err != nil {
		panic(err)
	}
	c.Write(data)
}

func sendHello(c *net.UnixConn) {
	ack := &zdsapi.ZdsHello{
		Version: zdsapi.Version_V1,
	}
	data, err := proto.Marshal(ack)
	if err != nil {
		panic(err)
	}
	err = c.SetWriteDeadline(time.Now().Add(time.Second))
	if err != nil {
		panic(err)
	}
	c.Write(data)
}

type fakePodCache struct {
	pods map[string]Netns
}

func (f fakePodCache) ReadCurrentPodSnapshot() map[string]Netns {
	return f.pods
}
