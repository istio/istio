package sfxclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"compress/gzip"
	"context"
	"github.com/golang/protobuf/proto"
	"github.com/signalfx/com_signalfx_metrics_protobuf"
	"github.com/signalfx/golib/datapoint"
	"github.com/signalfx/golib/errors"
	"github.com/signalfx/golib/event"
	"github.com/signalfx/golib/trace"
	"io"
	"sync"
)

const (
	// ClientVersion is the version of this library and is embedded into the user agent
	ClientVersion = "1.0"

	// IngestEndpointV2 is the v2 version of the signalfx ingest endpoint
	IngestEndpointV2 = "https://ingest.signalfx.com/v2/datapoint"

	// EventIngestEndpointV2 is the v2 version of the signalfx event endpoint
	EventIngestEndpointV2 = "https://ingest.signalfx.com/v2/event"

	// TraceIngestEndpointV1 is the v1 version of the signalfx trace endpoint
	TraceIngestEndpointV1 = "https://ingest.signalfx.com/v1/trace"

	// DefaultTimeout is the default time to fail signalfx datapoint requests if they don't succeed
	DefaultTimeout = time.Second * 5
)

// DefaultUserAgent is the UserAgent string sent to signalfx
var DefaultUserAgent = fmt.Sprintf("golib-sfxclient/%s (gover %s)", ClientVersion, runtime.Version())

// HTTPSink -
type HTTPSink struct {
	AuthToken          string
	UserAgent          string
	EventEndpoint      string
	DatapointEndpoint  string
	TraceEndpoint      string
	Client             http.Client
	protoMarshaler     func(pb proto.Message) ([]byte, error)
	jsonMarshal        func(v interface{}) ([]byte, error)
	DisableCompression bool
	zippers            sync.Pool

	stats struct {
		readingBody int64
	}
}

// SFXAPIError is returned when the API returns a status code other than 200.
type SFXAPIError struct {
	StatusCode   int
	ResponseBody string
}

func (se SFXAPIError) Error() string {
	return fmt.Sprintf("invalid status code %d", se.StatusCode)
}

func (h *HTTPSink) handleResponse(resp *http.Response, respErr error) (err error) {
	if respErr != nil {
		return errors.Annotatef(respErr, "failed to send/recieve http request")
	}
	defer func() {
		closeErr := errors.Annotate(resp.Body.Close(), "failed to close response body")
		err = errors.NewMultiErr([]error{err, closeErr})
	}()
	atomic.AddInt64(&h.stats.readingBody, 1)
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Annotate(err, "cannot fully read response body")
	}
	if resp.StatusCode != http.StatusOK {
		return SFXAPIError{
			StatusCode:   resp.StatusCode,
			ResponseBody: string(respBody),
		}
	}
	var bodyStr string
	err = json.Unmarshal(respBody, &bodyStr)
	if err != nil {
		return errors.Annotatef(err, "cannot unmarshal response body %s", respBody)
	}
	if bodyStr != "OK" {
		return errors.Errorf("invalid response body %s", bodyStr)
	}
	return nil
}

var _ Sink = &HTTPSink{}

// TokenHeaderName is the header key for the auth token in the HTTP request
const TokenHeaderName = "X-Sf-Token"

func (h *HTTPSink) doBottom(ctx context.Context, f func() (io.Reader, bool, error), contentType, endpoint string) error {
	if ctx.Err() != nil {
		return errors.Annotate(ctx.Err(), "context already closed")
	}
	body, compressed, err := f()
	if err != nil {
		return errors.Annotate(err, "cannot encode datapoints into "+contentType)
	}
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		return errors.Annotatef(err, "cannot parse new HTTP request to %s", endpoint)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(TokenHeaderName, h.AuthToken)
	req.Header.Set("User-Agent", h.UserAgent)
	req.Header.Set("Connection", "keep-alive")
	if compressed {
		req.Header.Set("Content-Encoding", "gzip")
	}

	return h.withCancel(ctx, req)
}

// AddDatapoints forwards the datapoints to SignalFx.
func (h *HTTPSink) AddDatapoints(ctx context.Context, points []*datapoint.Datapoint) (err error) {
	if len(points) == 0 {
		return nil
	}
	return h.doBottom(ctx, func() (io.Reader, bool, error) { return h.encodePostBodyProtobufV2(points) }, "application/x-protobuf", h.DatapointEndpoint)
}

var toMTMap = map[datapoint.MetricType]com_signalfx_metrics_protobuf.MetricType{
	datapoint.Counter:   com_signalfx_metrics_protobuf.MetricType_CUMULATIVE_COUNTER,
	datapoint.Count:     com_signalfx_metrics_protobuf.MetricType_COUNTER,
	datapoint.Enum:      com_signalfx_metrics_protobuf.MetricType_GAUGE,
	datapoint.Gauge:     com_signalfx_metrics_protobuf.MetricType_GAUGE,
	datapoint.Rate:      com_signalfx_metrics_protobuf.MetricType_GAUGE,
	datapoint.Timestamp: com_signalfx_metrics_protobuf.MetricType_GAUGE,
}

func toMT(mt datapoint.MetricType) com_signalfx_metrics_protobuf.MetricType {
	ret, exists := toMTMap[mt]
	if exists {
		return ret
	}
	panic(fmt.Sprintf("Unknown metric type: %d\n", mt))
}

func toEC(ec event.Category) com_signalfx_metrics_protobuf.EventCategory {
	// Check if the event.Category does not have a corresponding com_signalfx_metrics_protobuf.EventCategory
	if _, ok := com_signalfx_metrics_protobuf.EventCategory_name[int32(ec)]; !ok {
		panic(fmt.Sprintf("Unknown event category: %v\n", ec))
	}
	// Return the com_signalfx_metrics_protobuf.EventCategory
	return com_signalfx_metrics_protobuf.EventCategory(int32(ec))
}

func datumForPoint(pv datapoint.Value) *com_signalfx_metrics_protobuf.Datum {
	switch t := pv.(type) {
	case datapoint.IntValue:
		x := t.Int()
		return &com_signalfx_metrics_protobuf.Datum{IntValue: &x}
	case datapoint.FloatValue:
		x := t.Float()
		return &com_signalfx_metrics_protobuf.Datum{DoubleValue: &x}
	default:
		x := t.String()
		return &com_signalfx_metrics_protobuf.Datum{StrValue: &x}
	}
}

func mapToDimensions(dimensions map[string]string) []*com_signalfx_metrics_protobuf.Dimension {
	ret := make([]*com_signalfx_metrics_protobuf.Dimension, 0, len(dimensions))
	for k, v := range dimensions {
		if k == "" || v == "" {
			continue
		}
		// If someone knows a better way to do this, let me know.  I can't just take the &
		// of k and v because their content changes as the range iterates
		copyOfK := filterSignalfxKey(string([]byte(k)))
		copyOfV := string([]byte(v))
		ret = append(ret, &com_signalfx_metrics_protobuf.Dimension{
			Key:   &copyOfK,
			Value: &copyOfV,
		})
	}
	return ret
}

func filterSignalfxKey(str string) string {
	return strings.Map(runeFilterMap, str)
}

func runeFilterMap(r rune) rune {
	if unicode.IsDigit(r) || unicode.IsLetter(r) || r == '_' || r == '-' {
		return r
	}
	return '_'
}

func rawToProtobuf(raw interface{}) *com_signalfx_metrics_protobuf.PropertyValue {
	switch t := raw.(type) {
	case int64:
		return &com_signalfx_metrics_protobuf.PropertyValue{
			IntValue: &t,
		}
	case int:
		return &com_signalfx_metrics_protobuf.PropertyValue{
			IntValue: proto.Int64(int64(t)),
		}
	case float64:
		return &com_signalfx_metrics_protobuf.PropertyValue{
			DoubleValue: &t,
		}
	case bool:
		return &com_signalfx_metrics_protobuf.PropertyValue{
			BoolValue: &t,
		}
	case string:
		return &com_signalfx_metrics_protobuf.PropertyValue{
			StrValue: &t,
		}
	}
	return nil
}

func (h *HTTPSink) coreDatapointToProtobuf(point *datapoint.Datapoint) *com_signalfx_metrics_protobuf.DataPoint {
	m := point.Metric
	var ts int64
	if point.Timestamp.IsZero() {
		ts = 0
	} else {
		ts = point.Timestamp.UnixNano() / time.Millisecond.Nanoseconds()
	}
	mt := toMT(point.MetricType)
	dp := &com_signalfx_metrics_protobuf.DataPoint{
		Metric:     &m,
		Timestamp:  &ts,
		Value:      datumForPoint(point.Value),
		MetricType: &mt,
		Dimensions: mapToDimensions(point.Dimensions),
	}
	for k, v := range point.GetProperties() {
		kv := k
		pv := rawToProtobuf(v)
		if pv != nil && k != "" {
			dp.Properties = append(dp.Properties, &com_signalfx_metrics_protobuf.Property{
				Key:   &kv,
				Value: pv,
			})
		}
	}
	return dp
}

// avoid attempting to compress things that fit into a single ethernet frame
func (h *HTTPSink) getReader(b []byte) (io.Reader, bool, error) {
	var err error
	if !h.DisableCompression && len(b) > 1500 {
		buf := new(bytes.Buffer)
		w := h.zippers.Get().(*gzip.Writer)
		defer h.zippers.Put(w)
		w.Reset(buf)
		_, err = w.Write(b)
		if err == nil {
			err = w.Close()
			if err == nil {
				return buf, true, nil
			}
		}
	}
	return bytes.NewReader(b), false, err
}

func (h *HTTPSink) encodePostBodyProtobufV2(datapoints []*datapoint.Datapoint) (io.Reader, bool, error) {
	dps := make([]*com_signalfx_metrics_protobuf.DataPoint, 0, len(datapoints))
	for _, dp := range datapoints {
		dps = append(dps, h.coreDatapointToProtobuf(dp))
	}
	msg := &com_signalfx_metrics_protobuf.DataPointUploadMessage{
		Datapoints: dps,
	}
	body, err := h.protoMarshaler(msg)
	if err != nil {
		return nil, false, errors.Annotate(err, "protobuf marshal failed")
	}
	return h.getReader(body)
}

// AddEvents forwards the events to SignalFx.
func (h *HTTPSink) AddEvents(ctx context.Context, events []*event.Event) (err error) {
	if len(events) == 0 {
		return nil
	}
	return h.doBottom(ctx, func() (io.Reader, bool, error) { return h.encodePostBodyProtobufV2Events(events) }, "application/x-protobuf", h.EventEndpoint)
}

func (h *HTTPSink) encodePostBodyProtobufV2Events(events []*event.Event) (io.Reader, bool, error) {
	evs := make([]*com_signalfx_metrics_protobuf.Event, 0, len(events))
	for _, ev := range events {
		evs = append(evs, h.coreEventToProtobuf(ev))
	}
	msg := &com_signalfx_metrics_protobuf.EventUploadMessage{
		Events: evs,
	}
	body, err := h.protoMarshaler(msg)
	if err != nil {
		return nil, false, errors.Annotate(err, "protobuf marshal failed")
	}
	return h.getReader(body)
}

func (h *HTTPSink) coreEventToProtobuf(event *event.Event) *com_signalfx_metrics_protobuf.Event {
	var ts int64
	if event.Timestamp.IsZero() {
		ts = 0
	} else {
		ts = event.Timestamp.UnixNano() / time.Millisecond.Nanoseconds()
	}
	etype := event.EventType
	ecat := toEC(event.Category)
	ev := &com_signalfx_metrics_protobuf.Event{
		EventType:  &etype,
		Category:   &ecat,
		Dimensions: mapToDimensions(event.Dimensions),
		Properties: mapToProperties(event.Properties),
		Timestamp:  &ts,
	}
	return ev

}

func mapToProperties(properties map[string]interface{}) []*com_signalfx_metrics_protobuf.Property {
	var response = make([]*com_signalfx_metrics_protobuf.Property, 0, len(properties))
	for k, v := range properties {
		kv := k
		pv := rawToProtobuf(v)
		if pv != nil && k != "" {
			response = append(response, &com_signalfx_metrics_protobuf.Property{
				Key:   &kv,
				Value: pv,
			})
		}
	}
	return response
}

// AddSpans forwards the traces to SignalFx.
func (h *HTTPSink) AddSpans(ctx context.Context, traces []*trace.Span) (err error) {
	if len(traces) == 0 {
		return nil
	}
	return h.doBottom(ctx, func() (io.Reader, bool, error) {
		b, err := h.jsonMarshal(traces)
		if err != nil {
			return nil, false, errors.Annotate(err, "cannot encode traces into json")
		}
		return h.getReader(b)
	}, "application/json", h.TraceEndpoint)
}

// NewHTTPSink creates a default NewHTTPSink using package level constants as
// defaults, including an empty auth token.  If sending directly to SiganlFx, you will be required
// to explicitly set the AuthToken
func NewHTTPSink() *HTTPSink {
	return &HTTPSink{
		EventEndpoint:     EventIngestEndpointV2,
		DatapointEndpoint: IngestEndpointV2,
		TraceEndpoint:     TraceIngestEndpointV1,
		UserAgent:         DefaultUserAgent,
		Client: http.Client{
			Timeout: DefaultTimeout,
		},
		protoMarshaler: proto.Marshal,
		zippers: sync.Pool{New: func() interface{} {
			return gzip.NewWriter(nil)
		}},
		jsonMarshal: json.Marshal,
	}
}
