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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"k8s.io/client-go/util/jsonpath"

	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/snapshots"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

var (
	serverAddr             = flag.String("server", "localhost:9901", "The server address")
	collectionList         = flag.String("collections", "", "Comma separated list of collections to watch")
	useWellKnownTypes      = flag.Bool("use-wkt", false, "Use well known collections types")
	useWellKnownPilotTypes = flag.Bool("use-wkt-pilot", false, "Use well known collections for pilot")
	useWellKnownMixerTypes = flag.Bool("use-wkt-mixer", false, "Use well known collections for mixer")
	id                     = flag.String("id", "", "The node id for the client")
	output                 = flag.String("output", "short", "Output format. One of: long|short|stats|jsonpath=<template>")
	labels                 = flag.String("labels", "", "Comma separated key/value pairs, e.g. -labels=k1=v1,k2=v2")
)

var (
	shortHeader        = strings.Join([]string{"TYPE_URL", "INDEX", "KEY", "NAMESPACE", "NAME", "VERSION", "AGE"}, "\t")
	jsonpathHeader     = strings.Join([]string{"TYPE_URL", "INDEX", "KEY", "NAMESPACE", "NAME", "VERSION", "AGE", "JSONPATH"}, "\t")
	outputFormatWriter = tabwriter.NewWriter(os.Stdout, 4, 2, 4, ' ', 0)
	statsHeader        = strings.Join([]string{"COLLECTION", "APPLY", "ADD", "RE-ADD", "UPDATE", "DELETE"}, "\t")
)

type stat struct {
	apply  int64
	add    int64
	readd  int64 // added resource that already existed at the same version
	update int64
	delete int64

	// track the currently applied set of resources
	resources map[string]*mcp.Metadata
}

type updater struct {
	jp         *jsonpath.JSONPath
	stats      map[string]*stat
	totalApply int64

	sortedCollections []string // sorted list of sortedCollections
}

func asNamespaceAndName(key string) (string, string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

func (u *updater) printShortChange(ch *sink.Change) {
	fmt.Printf("Incoming apply: %v\n", ch.Collection)
	if len(ch.Objects) == 0 {
		return
	}

	now := time.Now()

	_, _ = fmt.Fprintln(outputFormatWriter, shortHeader)
	for i, o := range ch.Objects {
		age := ""
		if then, err := types.TimestampFromProto(o.Metadata.CreateTime); err == nil {
			age = now.Sub(then).Round(time.Millisecond).String()
		}
		namespace, name := asNamespaceAndName(o.Metadata.Name)

		parts := []string{
			o.TypeURL,
			strconv.Itoa(i),
			o.Metadata.Name,
			namespace, name,
			o.Metadata.Version,
			age,
		}

		_, _ = fmt.Fprintln(outputFormatWriter, strings.Join(parts, "\t"))
	}
	_ = outputFormatWriter.Flush()
}

// Update interface method implementation.
func (u *updater) printLongChange(ch *sink.Change) {
	fmt.Printf("Incoming apply: %v\n", ch.Collection)
	if len(ch.Objects) == 0 {
		return
	}

	for i, o := range ch.Objects {
		fmt.Printf("%s[%d]\n", ch.Collection, i)

		b, err := json.MarshalIndent(o, "  ", "  ")
		if err != nil {
			fmt.Printf("  Marshaling error: %v", err)
		} else {
			fmt.Printf("%s\n", string(b))
		}

		fmt.Printf("===============\n")
	}
}

func ageFromProto(now time.Time, t *types.Timestamp) string {
	age := ""
	if then, err := types.TimestampFromProto(t); err == nil {
		age = now.Sub(then).Round(time.Millisecond).String()
	}
	return age
}

func (u *updater) printJsonpathChange(ch *sink.Change) {
	fmt.Printf("Incoming apply: %v\n", ch.Collection)
	if len(ch.Objects) == 0 {
		return
	}

	now := time.Now()

	_, _ = fmt.Fprintln(outputFormatWriter, jsonpathHeader)
	for i, o := range ch.Objects {
		age := ageFromProto(now, o.Metadata.CreateTime)

		namespace, name := asNamespaceAndName(o.Metadata.Name)

		out := ""
		m := jsonpb.Marshaler{}
		resourceStr, err := m.MarshalToString(o.Body)
		if err == nil {
			queryObj := map[string]interface{}{}
			if err = json.Unmarshal([]byte(resourceStr), &queryObj); err == nil {
				var b bytes.Buffer
				if err = u.jp.Execute(&b, queryObj); err == nil {
					out = b.String()
				}
			}
		}

		parts := []string{
			o.TypeURL,
			strconv.Itoa(i),
			o.Metadata.Name,
			namespace, name,
			o.Metadata.Version,
			age,
			out,
		}

		_, _ = fmt.Fprintln(outputFormatWriter, strings.Join(parts, "\t"))
	}
	_ = outputFormatWriter.Flush()
}

func (u *updater) printStats(ch *sink.Change) {
	stats, ok := u.stats[ch.Collection]
	if !ok {
		fmt.Printf("unknown collection: %v\n", ch.Collection)
		return
	}

	now := time.Now()

	u.totalApply++
	stats.apply++

	added := make(map[string]*mcp.Metadata, len(ch.Objects))

	for _, obj := range ch.Objects {
		added[obj.Metadata.Name] = obj.Metadata
	}

	// update add/update stats
	for name, metadata := range added {
		if prev, found := stats.resources[name]; !found {
			stats.add++
			stats.resources[name] = metadata
		} else if metadata.Version == prev.Version {
			stats.readd++
			stats.resources[name] = metadata
		} else {
			stats.update++
			stats.resources[name] = metadata
		}
	}

	// update delete stats
	for name := range stats.resources {
		if _, ok = added[name]; !ok {
			stats.delete++
			delete(stats.resources, name)
		}
	}

	// clear the screen
	print("\033[H\033[2J")

	fmt.Printf("Update at %v: apply=%v\n", time.Now().Format(time.RFC822), u.totalApply)

	fmt.Println("Current resource versions")
	parts := []string{"COLLECTION", "NAME", "VERSION", "AGE"}
	_, _ = fmt.Fprintln(outputFormatWriter, strings.Join(parts, "\t"))

	for _, collection := range u.sortedCollections {
		st := u.stats[collection]

		// sort the list of resources for consistent output
		resources := make([]string, 0, len(st.resources))
		for name := range st.resources {
			resources = append(resources, name)
		}
		sort.Strings(resources)

		for _, name := range resources {
			metadata := st.resources[name]
			p := []string{collection, name, metadata.Version, ageFromProto(now, metadata.CreateTime)}
			_, _ = fmt.Fprintln(outputFormatWriter, strings.Join(p, "\t"))
		}
	}
	_ = outputFormatWriter.Flush()

	fmt.Printf("\n\n")

	fmt.Println("Change stats")
	_, _ = fmt.Fprintln(outputFormatWriter, statsHeader)
	for _, collection := range u.sortedCollections {
		s := u.stats[collection]
		p := []string{
			collection,
			strconv.FormatInt(s.apply, 10),
			strconv.FormatInt(s.add, 10),
			strconv.FormatInt(s.readd, 10),
			strconv.FormatInt(s.update, 10),
			strconv.FormatInt(s.delete, 10),
		}
		_, _ = fmt.Fprintln(outputFormatWriter, strings.Join(p, "\t"))
	}
	_ = outputFormatWriter.Flush()
}

// Apply implements Update
func (u *updater) Apply(ch *sink.Change) error {
	parts := strings.Split(*output, "=")
	switch parts[0] {
	case "long":
		u.printLongChange(ch)
	case "short":
		u.printShortChange(ch)
	case "stats":
		u.printStats(ch)
	case "jsonpath":
		u.printJsonpathChange(ch)
	default:
		fmt.Printf("Change %v\n", ch.Collection)
	}
	return nil
}

func main() {
	flag.Parse()

	var jp *jsonpath.JSONPath
	parts := strings.Split(*output, "=")
	if parts[0] == "jsonpath" {
		if len(parts) != 2 {
			panic(fmt.Sprintf("Unknown output option %q:\n", *output))
		}

		jp = jsonpath.New("default")
		if err := jp.Parse(parts[1]); err != nil {
			panic(fmt.Sprintf("Error parsing jsonpath: %v\n", err))
		}
	}

	sinkMetadata := make(map[string]string)
	if *labels != "" {
		pairs := strings.Split(*labels, ",")
		for _, pair := range pairs {
			s := strings.SplitN(pair, "=", 2)
			if len(s) != 2 {
				panic(fmt.Sprintf("invalid labels: %v %v\n", pair, s))
			}
			key, value := s[0], s[1]
			sinkMetadata[key] = value
		}
	}

	collectionsMap := make(map[string]struct{})

	if *collectionList != "" {
		for _, collection := range strings.Split(*collectionList, ",") {
			collectionsMap[collection] = struct{}{}
		}
	}

	for _, collection := range schema.MustGet().AllCollectionsInSnapshots(snapshots.SnapshotNames()) {

		switch {
		// pilot sortedCollections
		case strings.HasPrefix(collection, "istio/networking/"),
			strings.HasPrefix(collection, "istio/authentication/"),
			strings.HasPrefix(collection, "istio/config/v1alpha2/httpapispecs"),
			strings.HasPrefix(collection, "istio/config/v1alpha2/httpapispecbindings"),
			strings.HasPrefix(collection, "istio/mixer/v1/config/client"),
			strings.HasPrefix(collection, "istio/rbac"):
			if *useWellKnownTypes || *useWellKnownPilotTypes {
				collectionsMap[collection] = struct{}{}
			}

		// mixer sortedCollections
		case strings.HasPrefix(collection, "istio/policy/"),
			strings.HasPrefix(collection, "istio/config/"):
			if *useWellKnownTypes || *useWellKnownMixerTypes {
				collectionsMap[collection] = struct{}{}
			}

		// default (k8s?)
		default:
			if *useWellKnownTypes {
				collectionsMap[collection] = struct{}{}
			}
		}
	}

	// de-dup types
	collections := make([]string, 0, len(collectionsMap))
	for collection := range collectionsMap {
		collections = append(collections, collection)
	}

	sort.Strings(collections)

	conn, err := grpc.Dial(*serverAddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		panic(fmt.Sprintf("Error connecting to server: %v\n", err))
	}

	u := &updater{
		jp:                jp,
		stats:             make(map[string]*stat, len(collections)),
		sortedCollections: collections,
	}
	for _, collection := range collections {
		u.stats[collection] = &stat{resources: make(map[string]*mcp.Metadata)}
	}

	options := &sink.Options{
		CollectionOptions: sink.CollectionOptionsFromSlice(collections),
		Updater:           u,
		ID:                *id,
		Metadata:          sinkMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}

	ctx := context.Background()

	cl := mcp.NewResourceSourceClient(conn)
	c := sink.NewClient(cl, options)
	c.Run(ctx)
}
