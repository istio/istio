package mcp

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/snapshot"
	mcpsource "istio.io/istio/pkg/mcp/source"
	mcptest "istio.io/istio/pkg/mcp/testing"
)

type testData struct {
	info    resource.Info
	entry   proto.Message
	name    string
	version string
}

func allConfigsSnapshot(infos []testData, version string) snapshot.Snapshot {
	//createTime := time.Now()
	var createTime time.Time
	b := snapshot.NewInMemoryBuilder()
	// snapshot is supposed to be immutable. This is for testing
	for _, i := range infos {
		b.SetEntry(i.info.Collection.String(), i.name, i.version, createTime, map[string]string{}, map[string]string{}, i.entry)
		b.SetVersion(i.info.Collection.String(), version)
	}

	return b.Build()
}

func Test(t *testing.T) {
	data := []testData{
		{
			info:    metadata.Types.Get("istio/networking/v1alpha3/virtualservices"),
			name:    "test.vservice1",
			version: "1",
			entry: &v1alpha3.VirtualService{
				Hosts: []string{"localhost"},
			},
		},
		{
			info:    metadata.Types.Get("istio/networking/v1alpha3/virtualservices"),
			name:    "test.vservice2",
			version: "1",
			entry: &v1alpha3.VirtualService{
				Hosts:       []string{"somehost"},
				Gateways:    []string{"test"},
				Http:        nil,
				Tls:         nil,
				Tcp:         nil,
				ConfigScope: 0,
			},
		},
		{
			info:    metadata.Types.Get("istio/networking/v1alpha3/gateways"),
			name:    "test.gw1",
			version: "1",
			entry: &v1alpha3.Gateway{
				Servers:  nil,
				Selector: map[string]string{"istio": "ingressgateway"},
			},
		},
	}

	// start test mcp server
	server, err := mcptest.NewServer(0, mcpsource.CollectionOptionsFromSlice(metadata.Types.Collections()))
	if err != nil {
		t.Fatalf("failed to start MCP test server: %v", err)
	}

	// run galley Start for galley mcp client to connect to mcp server
	url := schemeURL(server.URL, false)
	fmt.Printf("connecting to: %q\n", url)
	underTest, _ := New(context.Background(), &creds.Options{}, url, "")
	events := make(chan resource.Event, defaultChanSize)
	if err := underTest.Start(func(e resource.Event) {
		events <- e
	}); err != nil {
		t.Fatalf("failed to start test server with: %v", err)
	}

	// Test Add
	// We should receive 2 Add events and a FullSync for Virtual Service
	// 1 Add and a FullSync for Gateway
	server.Cache.SetSnapshot(snapshot.DefaultGroup, allConfigsSnapshot(data, "1"))
	testAdd(t, events)

	//Test Update
	// We should see one update event in the channel
	data[0].version = "2"
	server.Cache.SetSnapshot(snapshot.DefaultGroup, allConfigsSnapshot(data, "2"))
	testUpdate(t, events)

	//Test AddAndUpdate
	// We should see one Update and one Add event in the channel
	data[1].version = "2"
	a := testData{
		info:    metadata.Types.Get("istio/networking/v1alpha3/gateways"),
		name:    "test.gw2",
		version: "1",
		entry: &v1alpha3.Gateway{
			Servers:  nil,
			Selector: map[string]string{"istio": "ingressgateway"},
		},
	}
	data = append(data, a)
	server.Cache.SetSnapshot(snapshot.DefaultGroup, allConfigsSnapshot(data, "3"))
	testAddAndUpdate(t, events)

	//Test Delete
	//We should see two Delete events in the channel
	data = append(data[:1], data[2])
	server.Cache.SetSnapshot(snapshot.DefaultGroup, allConfigsSnapshot(data, "4"))
	testDelete(t, events)

	//Test NoUpdate
	// We should not see any events in the channel
	server.Cache.SetSnapshot(snapshot.DefaultGroup, allConfigsSnapshot(data, "5"))
	testNoChange(t, events)

	// Mismatch in TypeURL
	testTypeURLMismatch(t, data)

	underTest.Stop()
}

func testTypeURLMismatch(t *testing.T, data []testData) {
	// Mismatch in TypeURL
	events := make(chan resource.Event, defaultChanSize)
	src := &source{
		lCache:             make(map[string]map[string]*cache),
		cashCollectionLock: make(map[string]*sync.Mutex),
		handler:            func(e resource.Event) { events <- e },
	}

	// Create object in local cache
	// There should be an event for Added and Fullsync
	c := &sink.Change{
		Collection: data[0].info.Collection.String(),
		Objects: []*sink.Object{
			{TypeURL: data[0].info.TypeURL.String(),
				Metadata: &mcp.Metadata{Name: data[0].name, Version: data[0].version},
				Body:     data[0].entry,
			},
		},
	}
	if err := src.Apply(c); err != nil {
		t.Fatalf("Apply returned error when not expected %v:", err)
	}
	results := make(chan resource.Event)
	tot := 0
	go func() {
		for {
			if tot == 2 {
				break
			}
			e := <-events
			switch e.Kind {
			case resource.Added:
				tot++
			case resource.FullSync:
				tot++
			default:
				t.Fatalf("Unexpected event received")
			}
		}
		results <- resource.Event{}
	}()
	wait := time.NewTimer(1 * time.Minute).C
	select {
	case <-wait:
		t.Fatalf("timed out waiting for all events")
	case r := <-results:
		if tot != 2 {
			t.Fatalf("too few results: %v", r)
		}
	}

	// Send an update to the object and another object with invalid typeurl
	c = &sink.Change{
		Collection: data[0].info.Collection.String(),
		Objects: []*sink.Object{
			{TypeURL: data[0].info.TypeURL.String(),
				Metadata: &mcp.Metadata{Name: data[0].name, Version: "9"},
				Body:     data[0].entry,
			},
			{TypeURL: "InvalidTypeURL",
				Metadata: &mcp.Metadata{Name: "testresource", Version: data[0].version},
				Body:     data[0].entry,
			},
		},
	}
	if err := src.Apply(c); err == nil {
		t.Fatalf("Apply expected to return error but returned nil")
	}

	// Send a new object and another object with invalid typeURL
	c = &sink.Change{
		Collection: data[0].info.Collection.String(),
		Objects: []*sink.Object{
			{TypeURL: data[0].info.TypeURL.String(),
				Metadata: &mcp.Metadata{Name: "testresource", Version: data[0].version},
				Body:     data[0].entry,
			},
			{TypeURL: "InvalidTypeURL",
				Metadata: &mcp.Metadata{Name: "some", Version: data[0].version},
				Body:     data[0].entry,
			},
		},
	}
	if err := src.Apply(c); err == nil {
		t.Fatalf("Apply expected to return error but returned nil")
	}

	// Invalid TypeURL at the response level
	// We should not see any events in the channel
	c = &sink.Change{Collection: "test",
		Objects: []*sink.Object{{TypeURL: "something"}},
	}
	if err := src.Apply(c); err == nil {
		t.Fatalf("Apply expected to return error but returned nil")
	}
	// All the Apply call above shouldn't have resulted in any event
	testNoChange(t, events)

	fmt.Println("event testypeURLMismatch pass")
}

func schemeURL(u *url.URL, secure bool) string {
	if secure {
		return fmt.Sprintf("mcps://%s:%s", u.Hostname(), u.Port())
	}
	return fmt.Sprintf("mcp://%s:%s", u.Hostname(), u.Port())
}

func testAdd(t *testing.T, events chan resource.Event) {
	results := make(chan resource.Event)
	vs := 0
	gw := 0
	tot := 0
	go func() {
		for {
			if tot == 5 {
				break
			}
			e := <-events
			fmt.Println("received event ", e)
			switch e.Kind {
			case resource.Added:
				if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/virtualservices" {
					vs++
				}
				if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/gateways" {
					gw++
				}
				tot++
			case resource.FullSync:
				tot++
			default:
				t.Fatalf("Unexpected event received")
			}
		}
		if gw != 1 || vs != 2 {
			t.Fatalf("Didn't receive all the Add events")
		}
		results <- resource.Event{}
	}()

	wait := time.NewTimer(1 * time.Minute).C
	select {
	case <-wait:
		t.Fatalf("timed out waiting for all events")
	case r := <-results:
		if tot != 5 {
			t.Fatalf("too few results: %v", r)
		}
	}
	fmt.Println("event testAdd pass")

}

func testUpdate(t *testing.T, events chan resource.Event) {
	results := make(chan resource.Event)
	vs := 0
	tot := 0
	go func() {
		for {
			if tot == 2 {
				break
			}
			e := <-events
			fmt.Println("received event ", e)
			switch e.Kind {
			case resource.Updated:
				if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/virtualservices" {
					vs++
				}
				tot++
			case resource.FullSync:
				tot++
			default:
				t.Fatalf("Unexpected event received")
			}
		}
		if vs != 1 {
			t.Fatalf("Didn't receive all the Update events")
		}
		results <- resource.Event{}
	}()

	wait := time.NewTimer(1 * time.Minute).C
	select {
	case <-wait:
		t.Fatalf("timed out waiting for all events")
	case r := <-results:
		if tot != 2 {
			t.Fatalf("too few results: %v", r)
		}
	}
	fmt.Println("event testUpdate pass")

}

func testAddAndUpdate(t *testing.T, events chan resource.Event) {
	results := make(chan resource.Event)
	vs := 0
	gw := 0
	tot := 0
	go func() {
		for {
			if tot == 4 {
				break
			}
			e := <-events
			fmt.Println("received event ", e)
			switch e.Kind {
			case resource.Added:
				if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/gateways" {
					gw++
					tot++
				} else {
					t.Fatalf("Unexpected event received")
				}
			case resource.Updated:
				if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/virtualservices" {
					vs++
					tot++
				} else {
					t.Fatalf("Unexpected event received")
				}
			case resource.FullSync:
				tot++
			default:
				t.Fatalf("Unexpected event received")
			}
		}
		if gw != 1 || vs != 1 {
			t.Fatalf("Didn't receive all the expected events")
		}
		results <- resource.Event{}
	}()

	wait := time.NewTimer(1 * time.Minute).C
	select {
	case <-wait:
		t.Fatalf("timed out waiting for all events")
	case r := <-results:
		if tot != 4 {
			t.Fatalf("too few results: %v", r)
		}
	}
	fmt.Println("event testAddAndUpdate pass")

}

func testDelete(t *testing.T, events chan resource.Event) {
	results := make(chan resource.Event)
	vs := 0
	gw := 0
	tot := 0
	go func() {
		for {
			if tot == 4 {
				break
			}
			e := <-events
			fmt.Println("received event ", e)
			switch e.Kind {
			case resource.Deleted:
				if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/virtualservices" {
					vs++
				} else if e.Entry.ID.Collection.String() == "istio/networking/v1alpha3/gateways" {
					gw++
				} else {
					t.Fatalf("Unexpected event received")
				}
				tot++
			case resource.FullSync:
				tot++
			default:
				t.Fatalf("Unexpected event received")
			}
		}
		if vs != 1 || gw != 1 {
			t.Fatalf("Didn't receive all the Deleted events")
		}
		results <- resource.Event{}
	}()

	wait := time.NewTimer(1 * time.Minute).C
	select {
	case <-wait:
		t.Fatalf("timed out waiting for all events")
	case r := <-results:
		if tot != 4 {
			t.Fatalf("too few results: %v", r)
		}
	}
	fmt.Println("event testDeleted pass")

}

func testNoChange(t *testing.T, events chan resource.Event) {
	go func() {
		for {
			e := <-events
			fmt.Println("received event ", e)
			switch e.Kind {
			default:
				t.Fatalf("Unexpected event received")
			}
		}
	}()

	wait := time.NewTimer(30 * time.Second).C
	<-wait
	fmt.Println("event testNoChange pass")
}

func TestCreds(t *testing.T) {
	// start test mcp server
	server, err := mcptest.NewServer(0, mcpsource.CollectionOptionsFromSlice(metadata.Types.Collections()))
	if err != nil {
		t.Fatalf("failed to start MCP test server: %v", err)
	}

	// run galley Start for galley mcp client to connect to mcp server
	url := schemeURL(server.URL, true)
	events := make(chan resource.Event, defaultChanSize)

	fmt.Printf("connecting to: %q\n", url)
	//Try with cred dummy dirs
	copts := &creds.Options{
		CertificateFile:   "/etc",
		KeyFile:           "/etc",
		CACertificateFile: "/etc",
	}
	underTest, _ := New(context.Background(), copts, url, "")
	if err := underTest.Start(func(e resource.Event) {
		events <- e
	}); err == nil {
		t.Fatalf("Expecting failed to dial but instead it was successful")
	}

	fmt.Printf("connecting to: %q\n", url)
	//Try with nonexistent cred file
	copts = &creds.Options{
		CertificateFile:   "/etc",
		KeyFile:           "/etc/certs",
		CACertificateFile: "/etc/certs/nonexistent",
	}
	underTest, _ = New(context.Background(), copts, url, "")
	if err := underTest.Start(func(e resource.Event) {
		events <- e
	}); err == nil {
		t.Fatalf("Expecting failed to dial but instead it was successful")
	}

	//Test with a valid cred file

}
