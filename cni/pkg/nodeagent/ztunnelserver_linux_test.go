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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/zdsapi"
)

var ztunnelTestCounter atomic.Uint32

func TestZtunnelServerHandleConn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := FakeZtunnelConnection()

	cache := &fakePodCache{}
	cacheCloser := fillCacheWithFakePods(cache, 2)
	defer cacheCloser()

	ch := make(chan UpdateRequest)
	myUUID := uuid.New()

	helloResp := &zdsapi.ZdsHello{}

	var wg sync.WaitGroup
	wg.Add(1)
	respData := &zdsapi.WorkloadResponse{}

	for uid, info := range cache.pods {
		add := &zdsapi.AddWorkload{
			WorkloadInfo: info.Workload(),
			Uid:          uid,
		}
		req := &zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Add{
				Add: add,
			},
		}
		locFD := int(info.NetnsCloser().Fd())
		conn.On("SendMsgAndWaitForAck", req, &locFD).Times(1).Return(respData, nil)
	}

	snapReq := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_SnapshotSent{
			SnapshotSent: &zdsapi.SnapshotSent{},
		},
	}

	conn.On("SendMsgAndWaitForAck", snapReq, (*int)(nil)).Times(1).Return(respData, nil)

	conn.On("ReadHello").Return(helloResp, nil)
	var updates <-chan UpdateRequest = ch
	conn.On("Updates").Return(updates)
	conn.On("UUID").Return(myUUID)
	conn.On("Close").Run(func(args mock.Arguments) {
		wg.Done()
	}).Return(nil)
	conn.On("CheckAlive", mock.Anything).Return(nil)

	srv := createStoppedServer(cache, uuid.New(), time.Second/100)

	go func() {
		srv.ztunServer.handleConn(ctx, conn)
	}()

	for uid, info := range cache.pods {
		r := &zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Del{
				Del: &zdsapi.DelWorkload{
					Uid: uid,
				},
			},
		}
		ret := make(chan updateResponse, 1)
		fdF := int(info.NetnsCloser().Fd())
		req := updateRequest{
			update: r,
			fd:     &fdF,
			resp:   ret,
		}

		conn.On("SendMsgAndWaitForAck", r, &fdF).Times(1).Return(respData, nil)
		ch <- req
		<-ret
	}

	wg.Wait()
	conn.AssertExpectations(t)
}

func TestZtunnelServerHandleConnWhenConnDies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := FakeZtunnelConnection()

	cache := &fakePodCache{}
	cacheCloser := fillCacheWithFakePods(cache, 2)
	defer cacheCloser()

	ch := make(chan UpdateRequest)
	myUUID := uuid.New()

	helloResp := &zdsapi.ZdsHello{}

	var wg sync.WaitGroup
	wg.Add(1)
	respData := &zdsapi.WorkloadResponse{}

	for uid, info := range cache.pods {
		add := &zdsapi.AddWorkload{
			WorkloadInfo: info.Workload(),
			Uid:          uid,
		}
		req := &zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Add{
				Add: add,
			},
		}
		locFD := int(info.NetnsCloser().Fd())
		conn.On("SendMsgAndWaitForAck", req, &locFD).Times(1).Return(respData, nil)
	}

	snapReq := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_SnapshotSent{
			SnapshotSent: &zdsapi.SnapshotSent{},
		},
	}

	conn.On("SendMsgAndWaitForAck", snapReq, (*int)(nil)).Times(1).Return(respData, nil)

	conn.On("ReadHello").Return(helloResp, nil)

	var updates <-chan UpdateRequest = ch
	conn.On("Updates").Return(updates)
	conn.On("UUID").Return(myUUID)
	conn.On("Close").Run(func(args mock.Arguments) {
		wg.Done()
	}).Return(nil)

	srv := createStoppedServer(cache, uuid.New(), time.Second/100)

	go func() {
		srv.ztunServer.handleConn(ctx, conn)
	}()

	for uid, info := range cache.pods {
		r := &zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Del{
				Del: &zdsapi.DelWorkload{
					Uid: uid,
				},
			},
		}
		ret := make(chan updateResponse, 1)
		fdF := int(info.NetnsCloser().Fd())
		req := updateRequest{
			update: r,
			fd:     &fdF,
			resp:   ret,
		}

		conn.On("SendMsgAndWaitForAck", r, &fdF).Times(1).Return(nil, fmt.Errorf("sendmsg: broken pipe"))
		ch <- req
		<-ret
		break
	}

	wg.Wait()
	conn.AssertExpectations(t)
}

func TestZtunnelServerHandleConnWhenKeepaliveFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := FakeZtunnelConnection()

	cache := &fakePodCache{}
	cacheCloser := fillCacheWithFakePods(cache, 2)
	defer cacheCloser()

	ch := make(chan UpdateRequest)
	myUUID := uuid.New()

	helloResp := &zdsapi.ZdsHello{}

	respData := &zdsapi.WorkloadResponse{}

	for uid, info := range cache.pods {
		add := &zdsapi.AddWorkload{
			WorkloadInfo: info.Workload(),
			Uid:          uid,
		}
		req := &zdsapi.WorkloadRequest{
			Payload: &zdsapi.WorkloadRequest_Add{
				Add: add,
			},
		}
		locFD := int(info.NetnsCloser().Fd())
		conn.On("SendMsgAndWaitForAck", req, &locFD).Times(1).Return(respData, nil)
	}

	snapReq := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_SnapshotSent{
			SnapshotSent: &zdsapi.SnapshotSent{},
		},
	}

	conn.On("SendMsgAndWaitForAck", snapReq, (*int)(nil)).Times(1).Return(respData, nil)

	conn.On("ReadHello").Return(helloResp, nil)
	var updates <-chan UpdateRequest = ch
	conn.On("Updates").Return(updates)
	conn.On("UUID").Return(myUUID)
	var doneWG sync.WaitGroup
	doneWG.Add(1)
	conn.On("Close").Run(func(args mock.Arguments) {
		doneWG.Done()
	}).Return(nil)
	conn.On("CheckAlive", mock.Anything).Return(fmt.Errorf("not alive"))

	srv := createStoppedServer(cache, uuid.New(), time.Second*1)

	go func() {
		srv.ztunServer.handleConn(ctx, conn)
	}()

	doneWG.Wait()
	conn.AssertExpectations(t)
}

func TestZtunnelSendsPodSnapshot(t *testing.T) {
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fixture := connect(ctx)
	defer fixture.podCloser()

	ztunClient := fixture.ztunClient
	uid := fixture.uid

	// we got a message from ztun, so it should have observed us being connected
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))

	m, fds := readRequest(t, ztunClient)

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

func TestZtunnelWriteErrorCausesConnToDrop(t *testing.T) {
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fixture := connect(ctx)
	defer fixture.podCloser()

	ztunClient := fixture.ztunClient

	// we got a message from ztun, so it should have observed us being connected
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))

	// Now close without responding
	ztunClient.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestMultipleConnectedZtunnelsGetEvents(t *testing.T) {
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := &fakePodCache{}

	cacheCloser := fillCacheWithFakePods(cache, 2)

	defer cacheCloser()

	srv := startServerWithPodCache(ctx, cache)
	defer srv.ztunServer.Close()

	// connect 1st zt client, and read snapshot
	client1 := connectZtClientToServer(srv.addr)
	sendHello(client1)
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))

	for i := 0; i < 2; i++ {
		m1, fds1 := readRequest(t, client1)
		_, ok := cache.pods[m1.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid]
		assert.Equal(t, ok, true)
		assert.Equal(t, len(fds1), 1)
		sendAck(client1)
	}

	m, fds := readRequest(t, client1)
	assert.Equal(t, len(fds), 0)

	sent := m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(client1)

	// Now, connect 2nd zt client, and read snapshot
	client2 := connectZtClientToServer(srv.addr)
	sendHello(client2)

	for i := 0; i < 2; i++ {
		m2, fds2 := readRequest(t, client2)
		_, ok := cache.pods[m2.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid]
		assert.Equal(t, ok, true)
		assert.Equal(t, len(fds2), 1)
		sendAck(client2)
	}

	m, fds = readRequest(t, client2)
	assert.Equal(t, len(fds), 0)

	sent = m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(client2)

	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(2))

	// Now, add a new pod. It should only go to the second, most recent client.
	errChan := make(chan error)
	firstNewPod, ns, tmpFileToClose := podAndNetns()
	defer tmpFileToClose.Close()

	go func() {
		errChan <- srv.ztunServer.PodAdded(ctx, firstNewPod, ns)
	}()

	// Synchronously process pod add for client2
	m3, fds3 := readRequest(t, client2)
	_, ok := cache.pods[m3.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid]
	assert.Equal(t, ok, false)
	assert.Equal(t, len(fds3), 1)
	sendAck(client2)

	// For delete, both should get the delete msg, even though only client2 got the add.

	go func() {
		errChan <- srv.ztunServer.PodDeleted(ctx, string(firstNewPod.ObjectMeta.UID))
	}()
	// Synchronously process pod delete for client1
	m4, fds4 := readRequest(t, client1)
	_, ok = cache.pods[m4.Payload.(*zdsapi.WorkloadRequest_Del).Del.Uid]
	assert.Equal(t, ok, false)
	assert.Equal(t, len(fds4), 0)
	sendAck(client1)

	// Synchronously process pod delete for client2
	m4, fds4 = readRequest(t, client2)
	_, ok = cache.pods[m4.Payload.(*zdsapi.WorkloadRequest_Del).Del.Uid]
	assert.Equal(t, ok, false)
	assert.Equal(t, len(fds4), 0)
	sendAck(client2)

	client1.Close()

	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))

	client2.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestZtunnelLatestConnFallsBackToPreviousIfNewestDisconnects(t *testing.T) {
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := &fakePodCache{}

	cacheCloser := fillCacheWithFakePods(cache, 2)

	defer cacheCloser()

	srv := startServerWithPodCache(ctx, cache)
	defer srv.ztunServer.Close()

	// connect 1st zt client, and read snapshot
	client1 := connectZtClientToServer(srv.addr)
	sendHello(client1)
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))

	for i := 0; i < 2; i++ {
		m1, fds1 := readRequest(t, client1)
		_, ok := cache.pods[m1.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid]
		assert.Equal(t, ok, true)
		assert.Equal(t, len(fds1), 1)
		sendAck(client1)
	}

	m, fds := readRequest(t, client1)
	assert.Equal(t, len(fds), 0)

	sent := m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(client1)

	// Now, connect 2nd zt client, and read snapshot
	client2 := connectZtClientToServer(srv.addr)
	sendHello(client2)

	for i := 0; i < 2; i++ {
		m2, fds2 := readRequest(t, client2)
		_, ok := cache.pods[m2.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid]
		assert.Equal(t, ok, true)
		assert.Equal(t, len(fds2), 1)
		sendAck(client2)
	}

	m, fds = readRequest(t, client2)
	assert.Equal(t, len(fds), 0)

	sent = m.Payload.(*zdsapi.WorkloadRequest_SnapshotSent).SnapshotSent
	if sent == nil {
		panic("expected snapshot sent")
	}
	sendAck(client2)

	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(2))

	// Now disconnect the newest client
	client2.Close()

	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))
	_, err := srv.ztunServer.conns.LatestConn()
	assert.Equal(t, (err == nil), true)

	// Now, add a new pod. Since client2 already disconnected, this should go to client 1
	errChan := make(chan error)
	firstNewPod, ns, tmpFileToClose := podAndNetns()
	defer tmpFileToClose.Close()

	go func() {
		errChan <- srv.ztunServer.PodAdded(ctx, firstNewPod, ns)
	}()

	// Synchronously process pod add for client2
	m3, fds3 := readRequest(t, client1)
	_, ok := cache.pods[m3.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid]
	assert.Equal(t, ok, false)
	assert.Equal(t, len(fds3), 1)
	sendAck(client1)

	go func() {
		errChan <- srv.ztunServer.PodDeleted(ctx, string(firstNewPod.ObjectMeta.UID))
	}()

	// Synchronously process pod delete for client1
	m4, fds4 := readRequest(t, client1)
	_, ok = cache.pods[m4.Payload.(*zdsapi.WorkloadRequest_Del).Del.Uid]
	assert.Equal(t, ok, false)
	assert.Equal(t, len(fds4), 0)
	sendAck(client1)

	client1.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestZtunnelRemovePod(t *testing.T) {
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
	defer ztunnelServer.Close()
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
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(1))
}

func TestZtunnelPodAdded(t *testing.T) {
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

	// now add the pod
	ztunnelServer := fixture.ztunServer
	defer ztunnelServer.Close()
	errChan := make(chan error)
	pod2, ns2, tmpFileToClose := podAndNetns()
	defer tmpFileToClose.Close()

	go func() {
		errChan <- ztunnelServer.PodAdded(ctx, pod2, ns2)
	}()
	// read the msg to delete from ztunnel
	m, fds = readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 1)
	assert.Equal(t, m.Payload.(*zdsapi.WorkloadRequest_Add).Add.Uid, string(pod2.UID))
	sendAck(ztunClient)

	assert.NoError(t, <-errChan)

	ztunClient.Close()
	// this will retry for a bit, so shouldn't flake
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func TestZtunnelPodKept(t *testing.T) {
	mt := monitortest.New(t)
	setupLogging()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pods := &fakePodCache{}

	pod, f, tmpFileToClose := podAndNetns()
	defer tmpFileToClose.Close()

	f.Close()
	pods.pods = map[string]WorkloadInfo{
		string(pod.UID): &workloadInfo{}, // simulate unknown netns
	}

	fixture := connectWithPods(ctx, pods)
	ztunClient := fixture.ztunClient
	// read initial pod add
	keep, fds := readRequest(t, ztunClient)
	assert.Equal(t, len(fds), 0)
	kept := keep.Payload.(*zdsapi.WorkloadRequest_Keep).Keep
	if kept.Uid != string(pod.UID) {
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

// podAndNetns returns a ref to the file - Go will close FDs when the File object is GC'd,
// so to prevent test glitches, we have to hang onto a reference for as long as we might need
// the FD to remain valid, or there's a risk the FD will be closed underneath us in test due to a GC.
//
// callers should `Close()` the fileref when they are done with it.
func podAndNetns() (*corev1.Pod, *fakeNs, *os.File) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		panic(err)
	}
	id := ztunnelTestCounter.Add(1)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("name-%d", id),
			UID:  types.UID(fmt.Sprintf("uid-%d", id)),
		},
		Spec:   corev1.PodSpec{},
		Status: corev1.PodStatus{},
	}
	return pod, newFakeNs(devNull.Fd()), devNull
}

func fillCacheWithFakePods(cache *fakePodCache, podCount int) func() {
	if cache.pods == nil {
		cache.pods = make(map[string]WorkloadInfo)
	}
	filesToClose := make([]*os.File, 0)
	for i := 0; i < podCount; i++ {
		uid, ns, closeFile := podAndNetns()
		filesToClose = append(filesToClose, closeFile)
		workload := workloadInfo{
			workload: podToWorkload(uid),
			netns:    ns,
		}
		cache.pods[string(uid.UID)] = workload
	}

	return func() {
		for _, f := range filesToClose {
			f.Close()
		}
	}
}

func connect(ctx context.Context) struct {
	ztunClient *net.UnixConn
	ztunServer *ztunnelServer
	uid        string
	podCloser  func()
} {
	cache := &fakePodCache{}

	podCloser := fillCacheWithFakePods(cache, 1)

	ret := connectWithPods(ctx, cache)
	return struct {
		ztunClient *net.UnixConn
		ztunServer *ztunnelServer
		uid        string
		podCloser  func()
	}{ztunClient: ret.ztunClient, ztunServer: ret.ztunServer, uid: maps.Keys(cache.pods)[0], podCloser: podCloser}
}

func connectWithPods(ctx context.Context, pods PodNetnsCache) struct {
	ztunClient *net.UnixConn
	ztunServer *ztunnelServer
} {
	server := startServerWithPodCache(ctx, pods)

	client := connectZtClientToServer(server.addr)

	// send hello
	sendHello(client)

	return struct {
		ztunClient *net.UnixConn
		ztunServer *ztunnelServer
	}{ztunClient: client, ztunServer: server.ztunServer}
}

func startServerWithPodCache(ctx context.Context, podCache PodNetnsCache) struct {
	ztunServer *ztunnelServer
	addr       string
} {
	// go uses @ instead of \0 for abstract unix sockets
	addr := fmt.Sprintf("@testaddr%d", ztunnelTestCounter.Add(1))
	ztServ, err := newZtunnelServer(addr, podCache, time.Second/10)
	if err != nil {
		panic(err)
	}
	go ztServ.Run(ctx)

	return struct {
		ztunServer *ztunnelServer
		addr       string
	}{ztunServer: ztServ, addr: addr}
}

func createStoppedServer(podCache PodNetnsCache, uuid uuid.UUID, keepalive time.Duration) struct {
	ztunServer *ztunnelServer
	addr       string
} {
	// go uses @ instead of \0 for abstract unix sockets
	addr := fmt.Sprintf("@testaddr%d", uuid)
	ztServ, err := newZtunnelServer(addr, podCache, keepalive)
	if err != nil {
		panic(err)
	}
	return struct {
		ztunServer *ztunnelServer
		addr       string
	}{ztunServer: ztServ, addr: addr}
}

func connectZtClientToServer(addr string) *net.UnixConn {
	// Now connect the fake client
	resolvedAddr, err := net.ResolveUnixAddr("unixpacket", addr)
	if err != nil {
		panic(err)
	}
	ztunClient, err := net.DialUnix("unixpacket", nil, resolvedAddr)
	if err != nil {
		panic(err)
	}

	return ztunClient
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
	pods map[string]WorkloadInfo
}

func (f fakePodCache) ReadCurrentPodSnapshot() map[string]WorkloadInfo {
	return maps.Clone(f.pods)
}
