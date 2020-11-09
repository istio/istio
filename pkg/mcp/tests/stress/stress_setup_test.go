//  Copyright Istio Authors
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
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	mcp "istio.io/api/mcp/v1alpha1"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/mcp/testing/groups"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/pkg/log"
)

var (
	iterations           = flag.Int("iterations", 1000, "number of update iterations")
	numCollections       = flag.Int("num_collections", 50, "number of collections")
	numClients           = flag.Int("num_clients", 20, "number of load numClients")
	randomSeed           = flag.Int64("rand_seed", 0, "initial random seed for generating datasets")
	numDatasets          = flag.Int("num_datasets", 1000, "number of datasets to pre-generate for the test")
	maxChangesPerDataset = flag.Int("max_changes", 10, "maximum number of changes per commonDataset")
	stepRate             = flag.Duration("step_rate", 1*time.Millisecond, "step rate for publishing new snapshot updates")
	minApplyDelay        = flag.Duration("min_apply_delay", 0, "minimum extra delay inserted during per-client apply.")
	maxApplyDelay        = flag.Duration("max_apply_delay", 0, "maximum extra delay inserted during per-client apply.")
	minUpdateDelay       = flag.Duration("min_update_delay", 0, "minimum extra delay inserted between snapshot updates")
	maxUpdateDelay       = flag.Duration("max_update_delay", 0, "maximum extra delay inserted between snapshot updates")
	nackRate             = flag.Float64("nack_rate", 0.01, "rate at which clients nack updates. Should be in the range [0,1]")
	serverIncSupported   = flag.Bool("server_inc_supported", false, "server supports incremental updates")
	clientIncPercentage  = flag.Float64("client_inc_rate", 0, "percentate of clients that request incremental updates [0,1]")
)

var (
	commonDataset []snapshot.Snapshot
)

var defaultCollections = generateCollectionNames(*numCollections, 0)

func generateCollectionNames(count, ordinal int) []string {
	var result []string
	for i := 0; i < count; i++ {
		result = append(result, fmt.Sprintf("c%driver/foo.bar.baz%driver", ordinal, i))
	}
	return result
}

type resourceVersion int64

func (v resourceVersion) String() string {
	return strconv.FormatInt(int64(v), 10)
}

func (v resourceVersion) next() resourceVersion {
	return v + 1
}

type resource struct {
	name        string
	version     resourceVersion
	createTime  time.Time
	labels      map[string]string
	annotations map[string]string
	body        proto.Message
}

func initDataset() {
	rnd := rand.New(rand.NewSource(*randomSeed))

	b := snapshot.NewInMemoryBuilder()

	resourcesByCollection := make(map[string][]*resource, len(defaultCollections))

	// Initial snapshot has valid resourceVersion info?
	for _, collection := range defaultCollections {
		version := base64.StdEncoding.EncodeToString(sha256.New().Sum(nil))
		b.SetVersion(collection, version)
	}

	prev := b.Build()
	commonDataset = append(commonDataset, prev)

	for i := 1; i < *numDatasets; i++ { // iterate over data sets
		b := prev.Builder()
		for _, collection := range defaultCollections { // iterate over collection
			max := rnd.Intn(*maxChangesPerDataset)
			for j := 0; j < max; j++ { // for each collection, iterate and apply a certain number of changes.
				switch rnd.Intn(3) {
				case 0: // Add a new resource
					e := &resource{
						name:       fmt.Sprintf("c_%v_%v", i, j),
						createTime: time.Now(),
						body:       &types.Empty{},
					}
					b.SetEntry(collection, e.name, e.version.String(), e.createTime, e.labels, e.annotations, e.body)
					resourcesByCollection[collection] = append(resourcesByCollection[collection], e)
				case 1: // Update an existing resource
					l := resourcesByCollection[collection]
					if len(l) == 0 {
						continue
					}
					e := l[rnd.Intn(len(l))]
					e.version = e.version.next() // bumping the version is sufficient. MCP doesn't inspect the body.
					b.SetEntry(collection, e.name, e.version.String(), e.createTime, e.labels, e.annotations, e.body)
				case 2: // Delete a resource
					l := resourcesByCollection[collection]
					if len(l) == 0 {
						continue
					}
					idx := rnd.Intn(len(l))
					e := l[idx]
					b.DeleteEntry(collection, e.name)
					l = append(l[:idx], l[idx+1:]...)
					resourcesByCollection[collection] = l
				}
			}

			sortedResources := resourcesByCollection[collection]
			sort.Slice(sortedResources, func(i, j int) bool { return sortedResources[i].name < sortedResources[j].name })

			// The response's resourceVersion is a hash of the name and version of each resource in the collection.
			h := sha256.New()
			for _, entry := range sortedResources {
				h.Write([]byte(entry.name))
				h.Write([]byte(entry.version.String()))
			}
			version := base64.StdEncoding.EncodeToString(h.Sum(nil))
			b.SetVersion(collection, version)
		}
		sn := b.Build()
		commonDataset = append(commonDataset, sn)
		prev = sn
	}
}

type clientState struct {
	// metadata indexed by collection and name
	resourcesByCollectionName map[string]map[string]*mcp.Metadata

	applied    int64
	badVersion int64
	acked      int64
	nacked     int64
	added      int64
	removed    int64
	inc        int64

	minApplyDelay time.Duration
	maxApplyDelay time.Duration
	nackRate      float64

	rnd *rand.Rand
}

type driver struct {
	options options
	rnd     *rand.Rand

	clientOpts *sink.Options
	serverOpts *source.Options

	nextDataset int

	ctx    context.Context
	cancel context.CancelFunc

	listener      net.Listener
	grpcServer    *grpc.Server
	serverAddress string
	cache         *snapshot.Cache

	limiter *rate.Limiter

	clientWG sync.WaitGroup
	clients  []*clientState
}

type options struct {
	stepRate            time.Duration
	numClients          int
	iterations          int
	minUpdateDelay      time.Duration
	maxUpdateDelay      time.Duration
	minApplyDelay       time.Duration
	maxApplyDelay       time.Duration
	nackRate            float64
	serverIncSupported  bool
	clientIncPercentage float64
	setupFn             func(d *driver)
	serverStepFn        func(d *driver)
}

func updateSnapshot(d *driver) {
	time.Sleep(randomTimeInRange(d.rnd, d.options.minUpdateDelay, d.options.maxUpdateDelay))

	sn := commonDataset[d.nextDataset]

	d.nextDataset = (d.nextDataset + 1) % len(commonDataset)

	d.cache.SetSnapshot(groups.Default, sn)
}

func defaultOptions() options {
	return options{
		stepRate:            *stepRate,
		numClients:          *numClients,
		minApplyDelay:       *minApplyDelay,
		maxApplyDelay:       *maxApplyDelay,
		minUpdateDelay:      *minUpdateDelay,
		maxUpdateDelay:      *maxUpdateDelay,
		nackRate:            *nackRate,
		iterations:          *iterations,
		serverIncSupported:  *serverIncSupported,
		serverStepFn:        updateSnapshot,
		clientIncPercentage: *clientIncPercentage,
	}
}

func newDriver(o options) (*driver, error) {
	d := &driver{
		options: o,
		rnd:     rand.New(rand.NewSource(*randomSeed)),
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())

	limit := rate.Every(o.stepRate)
	d.limiter = rate.NewLimiter(limit, 1)

	d.initOptions()
	if d.options.setupFn != nil {
		d.options.setupFn(d)
	}

	if err := d.initServer(); err != nil {
		return nil, err
	}

	for i := 0; i < d.options.numClients; i++ {
		if err := d.initClient(i); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *driver) initOptions() {
	d.serverOpts = &source.Options{
		Reporter:           monitoring.NewInMemoryStatsContext(),
		Watcher:            snapshot.New(groups.DefaultIndexFn),
		CollectionsOptions: source.CollectionOptionsFromSlice(defaultCollections),
		ConnRateLimiter:    mcprate.NewRateLimiter(time.Second*10, 10),
	}
	for i := range d.serverOpts.CollectionsOptions {
		co := &d.serverOpts.CollectionsOptions[i]
		co.Incremental = d.options.serverIncSupported
	}

	d.clientOpts = &sink.Options{
		ID:                "driver",
		CollectionOptions: sink.CollectionOptionsFromSlice(defaultCollections),
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	for i := range d.clientOpts.CollectionOptions {
		if d.rnd.Float64() <= d.options.clientIncPercentage {
			co := &d.clientOpts.CollectionOptions[i]
			co.Incremental = true
		}
	}
}

func randomTimeInRange(rand *rand.Rand, min, max time.Duration) time.Duration {
	timeRange := float64((min - max).Nanoseconds())
	return min + time.Duration(timeRange*rand.Float64())
}

// Close the driver
func (d *driver) Close() {
	d.cancel() // stop clients
	d.clientWG.Wait()

	d.grpcServer.GracefulStop()
	d.listener.Close()
}

func (d *driver) initClient(id int) error {
	conn, err := grpc.DialContext(d.ctx, d.serverAddress, grpc.WithInsecure())
	if err != nil {
		return err
	}

	cs := &clientState{
		resourcesByCollectionName: make(map[string]map[string]*mcp.Metadata),
		minApplyDelay:             d.options.minApplyDelay,
		maxApplyDelay:             d.options.maxApplyDelay,
		nackRate:                  d.options.nackRate,
		rnd:                       rand.New(rand.NewSource(*randomSeed)),
	}
	for _, collection := range d.clientOpts.CollectionOptions {
		cs.resourcesByCollectionName[collection.Name] = make(map[string]*mcp.Metadata)
	}

	cl := mcp.NewResourceSourceClient(conn)

	options := *d.clientOpts
	options.Updater = cs
	options.ID = strconv.Itoa(id)

	c := sink.NewClient(cl, &options)
	d.clients = append(d.clients, cs)

	d.clientWG.Add(1)
	go func() {
		c.Run(d.ctx)
		d.clientWG.Done()
		conn.Close()
	}()
	return nil
}

func (d *driver) initServer() error {
	d.cache = snapshot.New(groups.DefaultIndexFn)
	d.serverOpts.Watcher = d.cache

	s := source.NewServer(d.serverOpts, &source.ServerOptions{
		AuthChecker: server.NewAllowAllChecker(),
		RateLimiter: d.serverOpts.ConnRateLimiter.Create(),
	})

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	d.listener = l

	d.serverAddress = l.Addr().String()

	d.grpcServer = grpc.NewServer()
	mcp.RegisterResourceSourceServer(d.grpcServer, s)
	go d.grpcServer.Serve(d.listener)

	return nil
}

func (d *driver) step(t testing.TB) {
	t.Helper()

	if err := d.limiter.Wait(d.ctx); err != nil {
		t.Fatalf("Error waiting limiter: %v", err)
	}

	if d.options.serverStepFn != nil {
		d.options.serverStepFn(d)
	}
}

func (d *driver) run(t testing.TB) {
	t.Helper()

	runtime.GC()

	for i := 0; i < d.options.iterations; i++ {
		d.step(t)
	}

	var (
		acked      int64
		nacked     int64
		applied    int64
		badVersion int64
		added      int64
		removed    int64
		inc        int64
	)
	for _, cs := range d.clients {
		applied += cs.applied
		acked += cs.acked
		nacked += cs.nacked
		badVersion += cs.badVersion
		added += cs.added
		removed += cs.removed
		inc += cs.inc
	}

	ackedPercentage := float64(acked) / float64(applied) * 100
	nackedPercentage := float64(nacked) / float64(applied) * 100

	//
	if _, ok := t.(*testing.T); ok {
		w := tabwriter.NewWriter(os.Stdout, 4, 2, 4, ' ', 0)
		fmt.Fprintf(w, "updates published\t%v\n", d.options.iterations)
		fmt.Fprintf(w, "unique datasets\t%v\n", len(commonDataset))
		fmt.Fprintf(w, "updates applied by client\t%v\n", applied)
		fmt.Fprintf(w, "inc\t%v\n", inc)
		fmt.Fprintf(w, "added\t%v\n", added)
		fmt.Fprintf(w, "removed\t%v\n", removed)
		fmt.Fprintf(w, "acked\t%v (%.3v%%)\n", acked, ackedPercentage)
		fmt.Fprintf(w, "nacked\t%v (%.3v%%)\n", nacked, nackedPercentage)
		fmt.Fprintf(w, "inconsistent changes detected by client\t%v\n", badVersion)
		fmt.Fprintf(w, "GoRoutines\t%v\n", runtime.NumGoroutine())
		w.Flush()
	}

	if badVersion > 0 {
		t.Fatalf("inconsistent changes detected by client: %v", badVersion)
	}
}

func (c *clientState) Apply(ch *sink.Change) error {
	time.Sleep(randomTimeInRange(c.rnd, c.minApplyDelay, c.maxApplyDelay))

	resourcesByName, ok := c.resourcesByCollectionName[ch.Collection]
	if !ok || !ch.Incremental {
		resourcesByName = make(map[string]*mcp.Metadata)
	}

	if ch.Incremental {
		c.inc++

		resourcesByName, ok := c.resourcesByCollectionName[ch.Collection]
		if !ok {
			resourcesByName = make(map[string]*mcp.Metadata)
			c.resourcesByCollectionName[ch.Collection] = resourcesByName
		}

		for _, obj := range ch.Objects {
			resourcesByName[obj.Metadata.Name] = obj.Metadata
			c.added++
		}
		for _, removed := range ch.Removed {
			c.removed++
			delete(resourcesByName, removed)
		}
	} else {
		for _, obj := range ch.Objects {
			resourcesByName[obj.Metadata.Name] = obj.Metadata
		}
	}
	c.resourcesByCollectionName[ch.Collection] = resourcesByName

	sortedNames := make([]string, 0, len(resourcesByName))
	for _, md := range resourcesByName {
		sortedNames = append(sortedNames, md.Name)
	}
	sort.Strings(sortedNames)

	// the per-collection resourceVersion is a hash of the entire
	h := sha256.New()
	for _, name := range sortedNames {
		version := resourcesByName[name].Version
		h.Write([]byte(name))
		h.Write([]byte(version))
	}
	version := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if ch.SystemVersionInfo != version {
		c.badVersion++
	}

	c.applied++

	if c.rnd.Float64() <= c.nackRate {
		c.nacked++
		return errors.New("nack (fake)")
	}

	c.acked++
	return nil
}

func TestMain(m *testing.M) {
	flag.Parse()

	// reduce logging from grpc library
	var (
		infoW    = ioutil.Discard
		warningW = ioutil.Discard
		errorW   = os.Stderr
	)
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(infoW, warningW, errorW))

	// reduce logging from MCP library
	o := log.DefaultOptions()
	o.SetOutputLevel("mcp", log.NoneLevel)
	o.SetOutputLevel("default", log.NoneLevel)
	log.Configure(o)

	fmt.Println("Generating common dataset ...")
	initDataset()
	fmt.Println("Finished generating common dataset")

	// call flag.Parse() here if TestMain uses flags
	os.Exit(m.Run())
}
