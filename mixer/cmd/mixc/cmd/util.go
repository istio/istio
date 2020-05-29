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

package cmd

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	otgrpc "github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	ot "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/cmd/shared"
	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/pkg/tracing"
	"istio.io/pkg/attribute"
)

type clientState struct {
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
}

func createAPIClient(port string, tracingOptions *tracing.Options) (*clientState, error) {
	cs := clientState{}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	if tracingOptions.TracingEnabled() {
		_, err := tracing.Configure("mixer-client", tracingOptions)
		if err != nil {
			return nil, fmt.Errorf("could not build tracer: %v", err)
		}
		ot.InitGlobalTracer(ot.GlobalTracer())
		opts = append(opts, grpc.WithUnaryInterceptor(otgrpc.OpenTracingClientInterceptor(ot.GlobalTracer())))
	}

	var err error
	if cs.connection, err = grpc.Dial(port, opts...); err != nil {
		return nil, err
	}

	cs.client = mixerpb.NewMixerClient(cs.connection)
	return &cs, nil
}

func deleteAPIClient(cs *clientState) {
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
	m := attribute.NewStringMap("", make(map[string]string, 1), nil)
	for _, pair := range strings.Split(s, ";") {
		colon := strings.Index(pair, ":")
		if colon < 0 {
			return nil, fmt.Errorf("%s is not a valid key/value pair in the form key:value", pair)
		}

		k := pair[0:colon]
		v := pair[colon+1:]
		m.Set(k, v)
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

func process(b *attribute.MutableBag, dict *map[string]int32, s string, f convertFn) error {
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
			(*dict)[name] = int32(len(*dict))
		}
	}

	return nil
}

func parseAttributes(rootArgs *rootArgs) (*mixerpb.CompressedAttributes, []string, error) {
	b := attribute.GetMutableBag(nil)
	gb := make(map[string]int32)
	if err := process(b, &gb, rootArgs.stringAttributes, parseString); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.int64Attributes, parseInt64); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.doubleAttributes, parseFloat64); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.boolAttributes, parseBool); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.timestampAttributes, parseTime); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.durationAttributes, parseDuration); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.bytesAttributes, parseBytes); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.stringMapAttributes, parseStringMap); err != nil {
		return nil, nil, err
	}

	if err := process(b, &gb, rootArgs.attributes, parseAny); err != nil {
		return nil, nil, err
	}

	var attrs mixerpb.CompressedAttributes
	attr.ToProto(b, &attrs, nil, 0)

	dw := make([]string, len(gb))
	for k, v := range gb {
		dw[v] = k
	}
	return &attrs, dw, nil
}

func decodeError(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return "unknown"
	}
	result := st.Code().String()

	msg := st.Message()
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

/* Useful debugging aid, commented out until needed.
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

	_, _ = fmt.Fprint(tw, "  Attribute\tType\tValue\n")

	for _, name := range names {
		v, _ := b.Get(name)
		_, _ = fmt.Fprintf(tw, "  %s\t%T\t%v\n", name, v, v)
	}

	_ = tw.Flush()
	printf("%s", buf.String())
}
*/

func dumpReferencedAttributes(printf shared.FormatFn, attrs *mixerpb.ReferencedAttributes) {
	vals := make([]string, 0, len(attrs.AttributeMatches))
	for _, at := range attrs.AttributeMatches {
		out := attrs.Words[-1*at.Name-1]
		if at.MapKey != 0 {
			out += "::" + attrs.Words[-1*at.MapKey-1]
		}
		out += " " + at.Condition.String()
		vals = append(vals, out)
	}

	sort.Strings(vals)

	buf := bytes.Buffer{}
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprint(tw, "  Referenced Attributes\n")

	for _, v := range vals {
		fmt.Fprintf(tw, "    %s\n", v)
	}

	_ = tw.Flush()
	printf("%s", buf.String())

}
