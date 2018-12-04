// Copyright 2018 Istio Authors
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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"k8s.io/client-go/util/jsonpath"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/client"

	// Import the resource package to pull in all proto types.
	_ "istio.io/istio/galley/pkg/kube/converter/legacy"
	_ "istio.io/istio/galley/pkg/metadata"
)

var (
	serverAddr = flag.String("server", "127.0.0.1:9901", "The server address")
	typeURLs   = flag.String("types", "", "The fully qualified type URLs of resources to deploy")
	id         = flag.String("id", "", "The node id for the client")
	output     = flag.String("output", "short", "output format. One of: long|short|jsonpath=<template>")
)

var (
	shortHeader        = strings.Join([]string{"TYPE_URL", "INDEX", "KEY", "NAMESPACE", "NAME", "VERSION", "AGE"}, "\t")
	jsonpathHeader     = strings.Join([]string{"TYPE_URL", "INDEX", "KEY", "NAMESPACE", "NAME", "VERSION", "AGE", "JSONPATH"}, "\t")
	outputFormatWriter = tabwriter.NewWriter(os.Stdout, 4, 2, 4, ' ', 0)
)

type updater struct {
	jp *jsonpath.JSONPath
}

// TODO - remove once namespace is part of MCP metadata
func asNamespaceAndName(key string) (string, string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

func (u *updater) printLongChange(ch *client.Change) {
	fmt.Printf("Incoming change: %v\n", ch.TypeURL)
	for i, o := range ch.Objects {
		fmt.Printf("%s[%d]\n", ch.TypeURL, i)

		b, err := json.MarshalIndent(o, "  ", "  ")
		if err != nil {
			fmt.Printf("  Marshalling error: %v", err)
		} else {
			fmt.Printf("%s\n", string(b))
		}

		fmt.Printf("===============\n")
	}
}

func (u *updater) printShortChange(ch *client.Change) {
	now := time.Now()

	fmt.Fprintln(outputFormatWriter, shortHeader)
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

		fmt.Fprintln(outputFormatWriter, strings.Join(parts, "\t"))
	}
	outputFormatWriter.Flush()
}

func (u *updater) printJsonpathChange(ch *client.Change) {
	now := time.Now()

	fmt.Fprintln(outputFormatWriter, jsonpathHeader)
	for i, o := range ch.Objects {
		age := ""
		if then, err := types.TimestampFromProto(o.Metadata.CreateTime); err == nil {
			age = now.Sub(then).Round(time.Millisecond).String()
		}

		namespace, name := asNamespaceAndName(o.Metadata.Name)

		output := ""
		m := jsonpb.Marshaler{}
		resourceStr, err := m.MarshalToString(o.Resource)
		if err == nil {
			queryObj := map[string]interface{}{}
			if err := json.Unmarshal([]byte(resourceStr), &queryObj); err == nil {
				var b bytes.Buffer
				if err := u.jp.Execute(&b, queryObj); err == nil {
					output = b.String()
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
			output,
		}

		fmt.Fprintln(outputFormatWriter, strings.Join(parts, "\t"))
	}
	outputFormatWriter.Flush()
}

// Update interface method implementation.
func (u *updater) Apply(ch *client.Change) error {
	switch *output {
	case "long":
		u.printLongChange(ch)
	case "short":
		u.printShortChange(ch)
	default: // jsonpath
		u.printJsonpathChange(ch)
	}
	return nil
}

func main() {
	flag.Parse()

	u := &updater{}

	switch *output {
	case "long":
	case "short":
	default:
		parts := strings.Split(*output, "=")
		if len(parts) != 2 {
			fmt.Printf("unknown output option %q:\n", *output)
			os.Exit(-1)
		}

		if parts[0] != "jsonpath" {
			fmt.Printf("unknown output %q:\n", *output)
			os.Exit(-1)
		}

		u.jp = jsonpath.New("default")
		if err := u.jp.Parse(parts[1]); err != nil {
			fmt.Printf("Error parsing jsonpath: %v\n", err)
			os.Exit(-1)
		}
	}

	typeNames := strings.Split(*typeURLs, ",")

	conn, err := grpc.Dial(*serverAddr, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		os.Exit(-1)
	}

	cl := mcp.NewAggregatedMeshConfigServiceClient(conn)

	c := client.New(cl, typeNames, u, *id, map[string]string{}, client.NewStatsContext("mcpc"))
	c.Run(context.Background())
}
