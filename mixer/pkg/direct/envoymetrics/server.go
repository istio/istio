package envoymetrics

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	mpb "github.com/envoyproxy/go-control-plane/envoy/service/metrics/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mixerpb "istio.io/api/mixer/v1"
	prometheus "istio.io/gogo-genproto/prometheus"
	"istio.io/istio/pkg/log"
)

// TODO: support for buckets and quantiles is really really poor. Need a better story around that information.

const (
	serviceNodeSeparator = "~"
	kubePrefix           = "kubernetes://"
	protocol             = "envoy_metrics_service"

	// attribute names
	destinationIP   = "destination.ip"
	destinationUID  = "destination.uid"
	contextProtocol = "context.protocol"
	nodeID          = "envoy.api.v2.core.node.id"
	cluster         = "envoy.api.v2.core.node.cluster"
	metadata        = "envoy.api.v2.core.node.metadata"
	buildVersion    = "envoy.api.v2.core.node.build_version"
	region          = "envoy.api.v2.core.node.locality.region"
	zone            = "envoy.api.v2.core.node.locality.zone"
	subzone         = "envoy.api.v2.core.node.locality.subzone"
	familyName      = "io.prometheus.client.metricfamily.name"
	familyType      = "io.prometheus.client.metricfamily.kind" // use kind here, because 'type' will cause issues with the frontend for the expression lang
	metricLabels    = "io.prometheus.client.metric.labels"
	counterVal      = "io.prometheus.client.metric.counter.value"
	gaugeVal        = "io.prometheus.client.metric.gauge.value"
	summaryCount    = "io.prometheus.client.metric.summary.sample_count"
	summarySum      = "io.prometheus.client.metric.summary.sample_sum"
	quantiles       = "io.prometheus.client.metric.summary.quantiles"
	untypedVal      = "io.prometheus.client.metric.untyped.value"
	histogramCount  = "io.prometheus.client.metric.histogram.sample_count"
	histogramSum    = "io.prometheus.client.metric.histogram.sample_sum"
	buckets         = "io.prometheus.client.metric.histogram.buckets"
	metricTimestamp = "io.prometheus.client.metric.timestamp"
)

type (
	attributes map[string]interface{}

	server struct {
		attrCh   chan attributes
		reporter reporter
	}

	reporter interface {
		Report([]attributes)
	}

	mixerReporter struct {
		addr   string
		client mixerpb.MixerClient
	}

	loggingReporter struct{}
)

var (
	scope = log.RegisterScope("grpc", "grpc API server messages.", 0)

	baseWords = []string{contextProtocol, nodeID, cluster, metadata, region, zone, subzone, buildVersion, familyName, familyType, metricLabels,
		counterVal, gaugeVal, summaryCount, summarySum, quantiles, untypedVal, histogramCount, histogramSum, buckets, metricTimestamp}

	attrBatchPool sync.Pool
)

// NewMetricsServiceServer returns a new server for the envoy v2 metrics service.
func NewMetricsServiceServer(mixerAddr string, batchSize int64) (mpb.MetricsServiceServer, error) {
	attrBatchPool = sync.Pool{
		New: func() interface{} {
			return make([]attributes, 0, batchSize)
		},
	}
	ch := make(chan attributes, batchSize)

	var r reporter
	if len(mixerAddr) > 0 {
		scope.Warnf("Configuring Mixer reporter with address: %v", mixerAddr)
		client, err := mixerAPIClient(mixerAddr)
		if err != nil {
			return nil, fmt.Errorf("could not build mixer client: %v", err)
		}
		r = &mixerReporter{addr: mixerAddr, client: client}
	} else {
		r = &loggingReporter{}
	}

	go processAttributes(ch, batchSize, r)
	return &server{attrCh: ch}, nil
}

func (s *server) StreamMetrics(stream mpb.MetricsService_StreamMetricsServer) error {
	scope.Debug("new metrics service stream established")
	var idAttrs attributes
	for {
		msg, err := stream.Recv()
		if err != nil {
			if status.Code(err) != codes.Canceled {
				scope.Errorf("error in processing stream: %v", err)
			}
			return err
		}
		if msg.Identifier != nil {
			idAttrs = identiferAttributes(msg.Identifier)
			scope.Debugf("id attrs for stream: %#v", idAttrs)
		}
		for _, metricsFam := range msg.EnvoyMetrics {
			for _, m := range metricsFam.Metric {
				attrs := metricsAttributes(metricsFam, m)
				s.attrCh <- merge(idAttrs, attrs)
			}
		}
	}
}

func identiferAttributes(identifier *mpb.StreamMetricsMessage_Identifier) attributes {
	attrs := make(attributes, 7)

	node := identifier.GetNode()
	// TODO: figure out how best to serialize metadata
	if c := node.GetCluster(); len(c) > 0 {
		attrs[cluster] = c
	}
	if id := node.GetId(); len(id) > 0 {
		attrs[nodeID] = id

		nodeParts := strings.Split(id, serviceNodeSeparator)
		if len(nodeParts) != 4 {
			// TODO: Should this result in an error that closes the stream?
			scope.Warnf("node ID in unknown format: %s" + id)
		} else {
			// TODO: global dictionary usage ?
			attrs[destinationIP] = net.ParseIP(nodeParts[1])
			attrs[destinationUID] = kubePrefix + nodeParts[2]
		}
	}
	if bv := node.GetBuildVersion(); len(bv) > 0 {
		attrs[buildVersion] = bv
	}

	loc := node.GetLocality()
	if r := loc.GetRegion(); len(r) > 0 {
		attrs[region] = r
	}
	if z := loc.GetZone(); len(z) > 0 {
		attrs[zone] = z
	}
	if sz := loc.GetSubZone(); len(sz) > 0 {
		attrs[subzone] = sz
	}

	// IMPORTANT: Distinguishes Envoy metrics requests from proxy Report()s
	attrs[contextProtocol] = protocol

	// TODO: remove when destination.service is no longer the ID attribute
	attrs["destination.service"] = "placeholder.default.svc.cluster.local"

	return attrs
}

func metricsAttributes(fam *prometheus.MetricFamily, metric *prometheus.Metric) attributes {
	attrs := make(attributes, 5)

	if len(fam.GetName()) > 0 {
		attrs[familyName] = fam.Name
	}
	attrs[familyType] = fam.Type.String()

	labels := make(map[string]string, len(metric.Label))
	for _, pair := range metric.Label {
		labels[pair.Name] = pair.Value
	}
	if len(labels) > 0 {
		attrs[metricLabels] = labels
	}

	if c := metric.GetCounter(); c != nil {
		attrs[counterVal] = c.Value
	}
	if g := metric.GetGauge(); g != nil {
		attrs[gaugeVal] = g.Value
	}
	if u := metric.GetUntyped(); u != nil {
		attrs[untypedVal] = u.Value
	}
	if s := metric.GetSummary(); s != nil {
		attrs[summarySum] = s.SampleSum
		attrs[summaryCount] = s.SampleCount
		qMap := make(map[string]string, len(s.Quantile))
		for _, q := range s.Quantile {
			qMap[strconv.FormatFloat(q.Quantile, 'E', -1, 64)] = strconv.FormatFloat(q.Value, 'E', -1, 64)
		}
		attrs[quantiles] = qMap
	}
	if h := metric.GetHistogram(); h != nil {
		attrs[histogramSum] = h.SampleSum
		attrs[histogramCount] = h.SampleCount
		bMap := make(map[string]string, len(h.Bucket))
		for _, b := range h.Bucket {
			bMap[strconv.FormatFloat(b.UpperBound, 'E', -1, 64)] = strconv.FormatUint(b.CumulativeCount, 10)
		}
		attrs[buckets] = bMap
	}
	if t := metric.TimestampMs; t != 0 {
		attrs[metricTimestamp] = time.Unix(0, t*int64(time.Millisecond))
	}

	return attrs
}

func merge(first, second attributes) attributes {
	m := make(attributes, len(first)+len(second))

	for k, v := range first {
		m[k] = v
	}

	for k, v := range second {
		m[k] = v
	}

	return m
}

func processAttributes(ch chan attributes, batchSize int64, r reporter) {
	batch := attrBatchPool.Get().([]attributes)
	for {
		attrs := <-ch
		batch = append(batch, attrs)
		if len(batch) == int(batchSize) {
			go func(attrsList []attributes, reporter reporter) {
				reporter.Report(attrsList)
				releaseBatch(attrsList)
			}(batch, r)
			batch = attrBatchPool.Get().([]attributes)
		}
	}
}

func releaseBatch(batch []attributes) {
	batch = batch[:0]
	attrBatchPool.Put(batch)
}

func (l loggingReporter) Report(batch []attributes) {
	scope.Infof("Generated batch of requests of size: %d", len(batch))
	scope.Debugf("Batch: %v", batch)
}

func (m mixerReporter) Report(batch []attributes) {
	req := reportRequest(batch)
	_, err := m.client.Report(context.Background(), &req)
	if err != nil {
		scope.Errorf("error sending Report(): %v", err)
	}
}

func mixerAPIClient(addr string) (mixerpb.MixerClient, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	connection, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}

	return mixerpb.NewMixerClient(connection), nil
}

func reportRequest(attrList []attributes) mixerpb.ReportRequest {
	req := mixerpb.ReportRequest{}
	words, wordsMap := defaultWordsAndMap()
	for _, attrs := range attrList {
		ca := mixerpb.CompressedAttributes{}
		for k, v := range attrs {
			var index int32
			index, words, wordsMap = wordIndex(k, words, wordsMap)
			switch val := v.(type) {
			case string:
				if ca.Strings == nil {
					ca.Strings = make(map[int32]int32)
				}
				var valIndex int32
				valIndex, words, wordsMap = wordIndex(val, words, wordsMap)
				ca.Strings[index] = valIndex
			case bool:
				if ca.Bools == nil {
					ca.Bools = make(map[int32]bool)
				}
				ca.Bools[index] = val
			case time.Time:
				if ca.Timestamps == nil {
					ca.Timestamps = make(map[int32]time.Time)
				}
				ca.Timestamps[index] = val
			case time.Duration:
				if ca.Durations == nil {
					ca.Durations = make(map[int32]time.Duration)
				}

				ca.Durations[index] = val
			case []byte:
				if ca.Bytes == nil {
					ca.Bytes = make(map[int32][]byte)
				}

				ca.Bytes[index] = val
			case net.IP:
				if ca.Bytes == nil {
					ca.Bytes = make(map[int32][]byte)
				}

				ca.Bytes[index] = []byte(val)
			case float64:
				if ca.Doubles == nil {
					ca.Doubles = make(map[int32]float64)
				}

				ca.Doubles[index] = val
			case int64:
				if ca.Int64S == nil {
					ca.Int64S = make(map[int32]int64)
				}

				ca.Int64S[index] = val
			case map[string]string:
				if ca.StringMaps == nil {
					ca.StringMaps = make(map[int32]mixerpb.StringMap)
				}

				entries := make(map[int32]int32, len(val))
				for k, v := range val {
					var keyIndex, valIndex int32
					keyIndex, words, wordsMap = wordIndex(k, words, wordsMap)
					valIndex, words, wordsMap = wordIndex(v, words, wordsMap)
					entries[keyIndex] = valIndex
				}
				ca.StringMaps[index] = mixerpb.StringMap{
					Entries: entries,
				}
			}
		}
		req.Attributes = append(req.Attributes, ca)
	}
	req.DefaultWords = words
	return req
}

func wordIndex(candidate string, wordList []string, wordMap map[string]int) (int32, []string, map[string]int) {
	if index, ok := wordMap[candidate]; ok {
		return int32(index), wordList, wordMap
	}
	wordList = append(wordList, candidate)
	newIndex := -1 * len(wordList)
	wordMap[candidate] = newIndex
	return int32(newIndex), wordList, wordMap
}

func defaultWordsAndMap() ([]string, map[string]int) {
	defaultWords := make([]string, 0, len(baseWords))
	defaultWords = append(baseWords)

	wordMap := make(map[string]int, len(defaultWords))
	for i, v := range defaultWords {
		wordMap[v] = -(i + 1)
	}

	return defaultWords, wordMap
}
