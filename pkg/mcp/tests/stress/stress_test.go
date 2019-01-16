//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package stress

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	mcpclient "istio.io/istio/pkg/mcp/client"
	mcpserver "istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

const (
	RND_SEED          = 0
	ITERATIONS        = 10000
	COLLECTIONS_COUNT = 100
	//NO_CLIENTS         = 1
	NO_CLIENTS         = 10
	NO_DATASETS        = 100
	MAX_CHANGES_IN_SET = 100

	DEFAULT_STEP_RATE = time.Millisecond
)

func BenchmarkSteadyState_1Client(b *testing.B) {
	benchmarkSteadyState(1, b)
}

func BenchmarkSteadyState_10Clients(b *testing.B) {
	benchmarkSteadyState(10, b)
}

func benchmarkSteadyState(clients int, b *testing.B) {
	opts := defaultOptions()
	opts.clients = clients

	d, err := newDriver(opts)
	if err != nil {
		b.Fatalf("error creating driver: %v", err)
	}
	defer d.Close()

	d.run(b)
}

func BenchmarkUnknownType_1Client(b *testing.B) {
	benchmarkUnknownType(1, b)
}

func BenchmarkUnknownType_10Clients(b *testing.B) {
	benchmarkUnknownType(10, b)
}

func benchmarkUnknownType(clients int, b *testing.B) {
	opts := defaultOptions()
	opts.clients = clients
	opts.setupFn = func(d *driver) {
		d.clientOpts.CollectionOptions = sink.CollectionOptionsFromSlice(generateCollectionNames(COLLECTIONS_COUNT, 1))
	}
	d, err := newDriver(opts)
	if err != nil {
		b.Fatalf("error creating driver: %v", err)
	}
	defer d.Close()

	d.run(b)
}

//
//func TestStress(t *testing.T) {
//	opts := options{
//		rnd: rand.NewSource(RND_SEED),
//		serverStepFn: func(d *driver) {
//			d.steps++
//			log.Infof("Step: %d", d.steps)
//			d.cache.SetSnapshot("default", d.dataset[d.o.rnd.Int63()%int64(len(d.dataset))])
//			m := d.o.rnd.Int63() % int64(WAIT_STEP_MULTIPLIER_MAX)
//			dur := time.Duration(WAIT_STEP_SIZE * time.Duration(m))
//			time.Sleep(dur)
//		},
//	}
//
//	d, err := newDriver(opts)
//	if err != nil {
//		t.Fatalf("error creating driver: %v", err)
//	}
//	driver, err := newDriver(opts)
//	if err != nil {
//		t.Fatalf("error creating driver: %v", err)
//	}
//	defer driver.Close()
//	//
//	//fmt.Printf("STEPS: %d\n", d.steps)
//	//fmt.Printf("APPLIED: %d\n", d.applied)
//}

type driver struct {
	o options

	dataset []snapshot.Snapshot

	clientOpts *sink.Options
	serverOpts *source.Options

	ctx    context.Context
	cancel context.CancelFunc

	listener      net.Listener
	grpcServer    *grpc.Server
	sv            mcpserver.Server
	serverAddress string

	limiter *rate.Limiter

	clients []*mcpclient.Client
	applied int64
	steps   int64
}

type options struct {
	rnd          rand.Source
	stepRate     time.Duration
	clients      int
	setupFn      func(d *driver)
	serverStepFn func(d *driver)
	clientStepFn func(d *driver, i int, c *mcpclient.Client)
}

func defaultOptions() options {
	return options{
		rnd:      rand.NewSource(RND_SEED),
		stepRate: DEFAULT_STEP_RATE,
		clients:  NO_CLIENTS,
	}
}

var defaultCollections = generateCollectionNames(COLLECTIONS_COUNT, 0)

func generateCollectionNames(count, ordinal int) []string {
	var result []string
	for i := 0; i < count; i++ {
		result = append(result, fmt.Sprintf("c%d/type.googleapis.com/foo.bar.baz%d", ordinal, i))
	}
	return result
}

func newDriver(o options) (*driver, error) {
	d := &driver{
		o: o,
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())

	limit := rate.Every(o.stepRate)
	d.limiter = rate.NewLimiter(limit, 1)

	d.initDataset()
	d.initOptions()
	if d.o.setupFn != nil {
		d.o.setupFn(d)
	}

	err := d.initServer()
	if err != nil {
		return nil, err
	}

	for i := 0; i < d.o.clients; i++ {
		if err = d.initClient(i); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *driver) initDataset() {
	b := snapshot.NewInMemoryBuilder()
	prev := b.Build()
	d.dataset = append(d.dataset, prev)

	entries := make(map[string][]string)

	for i := 1; i < NO_DATASETS; i++ { // iterate over data sets
		b := prev.Builder()
		for _, t := range defaultCollections { // iterate over type urls
			max := d.o.rnd.Int63() % MAX_CHANGES_IN_SET
			for j := int64(0); j < max; j++ { // for each type url, iterate and apply a certain number of changes.

				switch d.o.rnd.Int63() % 3 {
				case 0: // Add a new entry
					name := fmt.Sprintf("ns/t_%d_%d", j, d.o.rnd)
					version := fmt.Sprintf("version%d", d.o.rnd)
					createTime := time.Unix(d.o.rnd.Int63(), d.o.rnd.Int63())
					b.SetEntry(t, name, version, createTime, map[string]string{}, map[string]string{}, &types.Empty{})
					b.SetVersion(t, version)
					l := entries[t]
					l = append(l, name)
					entries[t] = l

				case 1: // Update an existing entry
					l := entries[t]
					if len(l) == 0 {
						continue
					}
					idx := d.o.rnd.Int63() % int64(len(l))
					name := l[idx]
					version := fmt.Sprintf("version%d", d.o.rnd)
					createTime := time.Unix(d.o.rnd.Int63(), d.o.rnd.Int63())
					b.SetEntry(t, name, version, createTime, map[string]string{}, map[string]string{}, &types.Empty{})
					b.SetVersion(t, version)

				case 2: // Delete an entry
					l := entries[t]
					if len(l) == 0 {
						continue
					}
					idx := d.o.rnd.Int63() % int64(len(l))
					name := l[idx]
					b.DeleteEntry(t, name)
					l = append(l[:idx], l[idx+1:]...)
					entries[t] = l
				}
			}
		}
		sn := b.Build()
		d.dataset = append(d.dataset, sn)
		prev = sn
	}
}

func (d *driver) initOptions() {
	d.serverOpts = &source.Options{
		Reporter:           monitoring.NewInMemoryStatsContext(),
		Watcher:            snapshot.New(snapshot.DefaultGroupIndex),
		CollectionsOptions: source.CollectionOptionsFromSlice(defaultCollections),
		RetryPushDelay:     time.Nanosecond,
	}

	d.clientOpts = &sink.Options{
		ID:                "driver",
		Updater:           d,
		CollectionOptions: sink.CollectionOptionsFromSlice(defaultCollections),
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
}

// Close the driver
func (d *driver) Close() {
	d.cancel()
	d.grpcServer.GracefulStop()
}

func (d *driver) initClient(id int) error {
	conn, err := grpc.DialContext(d.ctx, d.serverAddress, grpc.WithInsecure())
	if err != nil {
		return err
	}

	cl := mcp.NewAggregatedMeshConfigServiceClient(conn)
	c := mcpclient.New(cl, d.clientOpts)
	d.clients = append(d.clients, c)
	go c.Run(d.ctx)
	return nil
}

func (d *driver) initServer() error {
	s := mcpserver.New(d.serverOpts, mcpserver.NewAllowAllChecker())

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	d.listener = l

	p := l.Addr().(*net.TCPAddr).Port

	d.serverAddress = fmt.Sprintf("localhost:%d", p)
	if err != nil {
		_ = l.Close()
		return err
	}

	d.grpcServer = grpc.NewServer()

	mcp.RegisterAggregatedMeshConfigServiceServer(d.grpcServer, s)

	go d.grpcServer.Serve(d.listener)
	return nil
}

func (d *driver) step(t testing.TB) {
	if err := d.limiter.Wait(d.ctx); err != nil {
		t.Fatalf("Error waiting limiter: %v", err)
	}

	if d.o.serverStepFn != nil {
		d.o.serverStepFn(d)
	}
	if d.o.clientStepFn != nil {
		for i, c := range d.clients {
			d.o.clientStepFn(d, i, c)
		}
	}
	d.steps++
}

func (d *driver) run(t testing.TB) {
	o := log.DefaultOptions()
	o.SetOutputLevel("mcp", log.NoneLevel)
	o.SetOutputLevel("default", log.NoneLevel)
	log.Configure(o)

	runtime.GC()

	if b, ok := t.(*testing.B); ok {
		b.Run("iter", func(b *testing.B) {
			d.runIterations(b, b.N)
		})
	} else {
		d.runIterations(t, ITERATIONS)
	}

	fmt.Printf("STEPS: %d\n", d.steps)
	fmt.Printf("APPLIED: %d\n", d.applied)
	fmt.Printf("No GoRoutines: %d\n", runtime.NumGoroutine())
}

func (d *driver) runIterations(t testing.TB, iter int) {
	for i := 0; i < iter; i++ {
		d.step(t)
	}
}

func (d *driver) Apply(*sink.Change) error {
	d.applied++
	log.Infof("Apply: %d", d.applied)

	switch d.o.rnd.Int63() % 2 {
	case 1:
		return fmt.Errorf("cheese not found.\n")
	}
	return nil
}
