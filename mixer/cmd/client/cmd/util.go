// Copyright 2017 Istio Authors
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

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"
	otgrpc "github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	ot "github.com/opentracing/opentracing-go"
	zt "github.com/openzipkin/zipkin-go-opentracing"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/cmd/shared"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/tracing/zipkin"
)

type clientState struct {
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
}

func createAPIClient(port string, enableTracing bool) (*clientState, error) {
	cs := clientState{}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	if enableTracing {
		tracer, err := zt.NewTracer(zipkin.IORecorder(os.Stdout))
		if err != nil {
			return nil, err
		}
		ot.InitGlobalTracer(tracer)
		opts = append(opts, grpc.WithUnaryInterceptor(otgrpc.OpenTracingClientInterceptor(tracer)))
	}

	var err error
	if cs.connection, err = grpc.Dial(port, opts...); err != nil {
		return nil, err
	}

	cs.client = mixerpb.NewMixerClient(cs.connection)
	return &cs, nil
}

func deleteAPIClient(cs *clientState) {
	// TODO: This is to compensate for this bug: https://github.com/grpc/grpc-go/issues/1059
	//       Remove this delay once that bug is fixed.
	time.Sleep(50 * time.Millisecond)

	_ = cs.connection.Close()
	cs.client = nil
	cs.connection = nil
}

func parseString(s string) (interface{}, error)  { return s, nil }
func parseInt64(s string) (interface{}, error)   { return strconv.ParseInt(s, 10, 64) }
func parseFloat64(s string) (interface{}, error) { return strconv.ParseFloat(s, 64) }
func parseBool(s string) (interface{}, error)    { return strconv.ParseBool(s) }

func parseTime(s string) (interface{}, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func parseDuration(s string) (interface{}, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}

	return d, nil
}

func parseBytes(s string) (interface{}, error) {
	var bytes []uint8
	for _, seg := range strings.Split(s, ":") {
		b, err := strconv.ParseUint(seg, 16, 8)
		if err != nil {
			return nil, err
		}
		bytes = append(bytes, uint8(b))
	}
	return bytes, nil
}

func parseStringMap(s string) (interface{}, error) {
	m := make(map[string]string)
	for _, pair := range strings.Split(s, ";") {
		colon := strings.Index(pair, ":")
		if colon < 0 {
			return nil, fmt.Errorf("%s is not a valid key/value pair in the form key:value", pair)
		}

		k := pair[0:colon]
		v := pair[colon+1:]
		m[k] = v
	}

	return m, nil
}

func parseAny(s string) (interface{}, error) {
	// auto-sense the type of attributes based on being able to parse the value
	if val, err := parseInt64(s); err == nil {
		return val, nil
	} else if val, err := parseFloat64(s); err == nil {
		return val, nil
	} else if val, err := parseBool(s); err == nil {
		return val, nil
	} else if val, err := parseTime(s); err == nil {
		return val, nil
	} else if val, err := parseDuration(s); err == nil {
		return val, nil
	} else if val, err := parseBytes(s); err == nil {
		return val, nil
	} else if val, err := parseStringMap(s); err == nil {
		return val, nil
	}
	return s, nil
}

type convertFn func(string) (interface{}, error)

func process(b *attribute.MutableBag, s string, f convertFn) error {
	if len(s) > 0 {
		for _, seg := range strings.Split(s, ",") {
			eq := strings.Index(seg, "=")
			if eq < 0 {
				return fmt.Errorf("attribute value %v does not include an = sign", seg)
			}
			if eq == 0 {
				return fmt.Errorf("attribute value %v does not contain a valid name", seg)
			}
			name := seg[0:eq]
			value := seg[eq+1:]

			// convert
			nv, err := f(value)
			if err != nil {
				return err
			}

			// add to results
			b.Set(name, nv)
		}
	}

	return nil
}

func parseAttributes(rootArgs *rootArgs) (*mixerpb.CompressedAttributes, error) {
	b := attribute.GetMutableBag(nil)

	if err := process(b, rootArgs.stringAttributes, parseString); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.int64Attributes, parseInt64); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.doubleAttributes, parseFloat64); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.boolAttributes, parseBool); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.timestampAttributes, parseTime); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.durationAttributes, parseDuration); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.bytesAttributes, parseBytes); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.stringMapAttributes, parseStringMap); err != nil {
		return nil, err
	}

	if err := process(b, rootArgs.attributes, parseAny); err != nil {
		return nil, err
	}

	var attrs mixerpb.CompressedAttributes
	b.ToProto(&attrs, nil, 0)

	return &attrs, nil
}

func decodeError(err error) string {
	result := grpc.Code(err).String()

	msg := grpc.ErrorDesc(err)
	if msg != "" {
		result = result + " (" + msg + ")"
	}

	return result
}

func decodeStatus(status rpc.Status) string {
	result, ok := rpc.Code_name[status.Code]
	if !ok {
		result = fmt.Sprintf("Code %d", status.Code)
	}

	if status.Message != "" {
		result = result + " (" + status.Message + ")"
	}

	return result
}

func dumpAttributes(printf, fatalf shared.FormatFn, attrs *mixerpb.CompressedAttributes) {
	if attrs == nil {
		return
	}

	b, err := attribute.GetBagFromProto(attrs, nil)
	if err != nil {
		fatalf(fmt.Sprintf("  Unable to decode returned attributes: %v", err))
	}

	names := b.Names()
	if len(names) == 0 {
		return
	}

	sort.Strings(names)

	buf := bytes.Buffer{}
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprint(tw, "  Attribute\tType\tValue\n")

	for _, name := range names {
		v, _ := b.Get(name)
		fmt.Fprintf(tw, "  %s\t%T\t%v\n", name, v, v)
	}

	_ = tw.Flush()
	printf("%s", buf.String())
}
