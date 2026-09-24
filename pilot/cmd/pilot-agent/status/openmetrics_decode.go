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

package status

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	commonModel "github.com/prometheus/common/model"
	promExemplar "github.com/prometheus/prometheus/model/exemplar"
	promLabels "github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// decodeOpenMetricsToFamilies parses an OpenMetrics text body into a slice of
// *dto.MetricFamily, preserving the OM-specific signals that the prometheus/common
// expfmt text decoder silently drops or misinterprets: exemplars, _created
// timestamps, # UNIT, INFO, STATESET. See prometheus/common#812.
//
// The adapter uses prometheus/prometheus/model/textparse.OpenMetricsParser (the
// canonical OM parser, already a direct dep of istio) and assembles the per-entry
// stream into MetricFamily values keyed by metric family name. Bucket exemplars
// are attached to the matching histogram bucket; counter exemplars to the
// matching counter sample. _created timestamps are surfaced via the
// Counter/Histogram/Summary CreatedTimestamp field and the _created series itself
// is suppressed via WithOMParserSTSeriesSkipped.
//
// Malformed bodies surface as a returned error along with whatever families were
// successfully parsed before the failure, matching the failure semantics of the
// expfmt text decoder used elsewhere in the merger.
func decodeOpenMetricsToFamilies(body io.Reader) ([]*dto.MetricFamily, error) {
	buf, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("openmetrics decode: read body: %w", err)
	}
	if len(buf) == 0 {
		return nil, nil
	}

	parser := textparse.NewOpenMetricsParser(buf, promLabels.NewSymbolTable())
	a := &omAssembler{
		families:   map[string]*dto.MetricFamily{},
		createdSec: map[string]float64{},
		metricIdx:  map[string]map[string]int{},
	}

	for {
		entry, perr := parser.Next()
		if perr != nil {
			if errors.Is(perr, io.EOF) {
				break
			}
			return a.materialize(), fmt.Errorf("openmetrics decode: %w", perr)
		}
		if err := a.handle(parser, entry); err != nil {
			return a.materialize(), fmt.Errorf("openmetrics decode: %w", err)
		}
	}
	return a.materialize(), nil
}

// omAssembler accumulates per-entry parser output into MetricFamily values,
// preserving insertion order across families so the merged output is stable
// across scrapes.
//
// createdSec maps family-name → _created timestamp in seconds (with fractional
// component preserved). We capture _created lines as standalone EntrySeries
// emissions during the first pass and resolve them onto Counter/Histogram/
// Summary CreatedTimestamp at family-end. This avoids the per-sample
// StartTimestamp() peek-ahead, which is O(n) in the OM body length and would
// be O(n²) overall for a 10K-series body.
//
// metricIdx maps family-name → (label-key → index in fam.Metric). This keeps
// findOrCreateMetric O(1) per call even when a family has thousands of
// distinct label sets.
type omAssembler struct {
	families   map[string]*dto.MetricFamily
	order      []string
	createdSec map[string]float64
	metricIdx  map[string]map[string]int
}

func (a *omAssembler) getOrCreate(name string) *dto.MetricFamily {
	if fam, ok := a.families[name]; ok {
		return fam
	}
	fam := &dto.MetricFamily{Name: proto.String(name)}
	a.families[name] = fam
	a.order = append(a.order, name)
	a.metricIdx[name] = map[string]int{}
	return fam
}

func (a *omAssembler) materialize() []*dto.MetricFamily {
	out := make([]*dto.MetricFamily, 0, len(a.order))
	for _, name := range a.order {
		fam := a.families[name]
		if ts, ok := a.createdSec[name]; ok {
			a.applyCreated(fam, ts)
		}
		out = append(out, fam)
	}
	return out
}

// applyCreated sets the CreatedTimestamp on every Counter/Histogram/Summary
// metric in fam to the given seconds-since-epoch (with fractional component).
func (a *omAssembler) applyCreated(fam *dto.MetricFamily, sec float64) {
	pts := timestamppb.New(time.UnixMilli(secondsToMillis(sec)))
	switch fam.GetType() {
	case dto.MetricType_COUNTER:
		for _, m := range fam.Metric {
			if m.Counter != nil {
				m.Counter.CreatedTimestamp = pts
			}
		}
	case dto.MetricType_HISTOGRAM:
		for _, m := range fam.Metric {
			if m.Histogram != nil {
				m.Histogram.CreatedTimestamp = pts
			}
		}
	case dto.MetricType_SUMMARY:
		for _, m := range fam.Metric {
			if m.Summary != nil {
				m.Summary.CreatedTimestamp = pts
			}
		}
	}
}

// secondsToMillis converts an OpenMetrics timestamp (seconds-since-epoch as a
// float64 with millisecond fraction) into a millisecond Unix timestamp.
func secondsToMillis(sec float64) int64 {
	return int64(sec * 1000)
}

func (a *omAssembler) handle(parser textparse.Parser, entry textparse.Entry) error {
	switch entry {
	case textparse.EntryHelp:
		name, help := parser.Help()
		a.getOrCreate(string(name)).Help = proto.String(string(help))
	case textparse.EntryType:
		name, mt := parser.Type()
		fam := a.getOrCreate(string(name))
		fam.Type = toDTOMetricType(mt).Enum()
	case textparse.EntryUnit:
		name, unit := parser.Unit()
		if len(unit) > 0 {
			a.getOrCreate(string(name)).Unit = proto.String(string(unit))
		}
	case textparse.EntrySeries:
		return a.ingestSeries(parser)
	case textparse.EntryComment, textparse.EntryHistogram, textparse.EntryInvalid:
		// Comments are not preserved by the dto model. Native histograms are
		// proto-only and never appear in an OM text body. EntryInvalid is reached
		// only with a non-nil error (handled by Next), but keep the case for clarity.
	}
	return nil
}

// ingestSeries decodes the current EntrySeries into the appropriate MetricFamily,
// resolving the parent family by stripping standard suffixes (_total, _count,
// _sum, _bucket) and using the # TYPE metadata observed previously. Exemplars
// attach to the matching Counter or Bucket. _created series are captured into
// the per-family createdSec map for resolution at family-end (see
// applyCreated).
func (a *omAssembler) ingestSeries(parser textparse.Parser) error {
	_, tsPtr, val := parser.Series()

	var lbls promLabels.Labels
	parser.Labels(&lbls)
	metricName := lbls.Get(commonModel.MetricNameLabel)
	if metricName == "" {
		return errors.New("series has no metric name")
	}

	familyName, kind := a.resolveFamily(metricName)

	// _created series do not emit a sample; they only carry a created timestamp
	// for the family.
	if kind == suffixCreated {
		a.createdSec[familyName] = val
		return nil
	}

	fam := a.getOrCreate(familyName)

	// If the family arrived series-first (TYPE not seen yet), infer from suffix.
	if fam.Type == nil {
		t := inferTypeFromSuffix(kind)
		fam.Type = t.Enum()
	}

	var exemplarOpt *dto.Exemplar
	var ex promExemplar.Exemplar
	if parser.Exemplar(&ex) {
		exemplarOpt = toDTOExemplar(&ex)
	}

	switch fam.GetType() {
	case dto.MetricType_COUNTER:
		lp := stripStandardLabels(lbls, dto.MetricType_COUNTER)
		metric := findOrCreateMetric(fam, a.metricIdx[familyName], lp, tsPtr)
		if metric.Counter == nil {
			metric.Counter = &dto.Counter{}
		}
		metric.Counter.Value = proto.Float64(val)
		if exemplarOpt != nil {
			metric.Counter.Exemplar = exemplarOpt
		}

	case dto.MetricType_GAUGE:
		lp := stripStandardLabels(lbls, dto.MetricType_GAUGE)
		metric := findOrCreateMetric(fam, a.metricIdx[familyName], lp, tsPtr)
		if metric.Gauge == nil {
			metric.Gauge = &dto.Gauge{}
		}
		metric.Gauge.Value = proto.Float64(val)

	case dto.MetricType_HISTOGRAM:
		lp := stripStandardLabels(lbls, dto.MetricType_HISTOGRAM)
		metric := findOrCreateMetric(fam, a.metricIdx[familyName], lp, tsPtr)
		if metric.Histogram == nil {
			metric.Histogram = &dto.Histogram{}
		}
		switch kind {
		case suffixBucket:
			leStr := lbls.Get("le")
			le, perr := strconv.ParseFloat(leStr, 64)
			if perr != nil {
				return fmt.Errorf("invalid histogram bucket le=%q: %w", leStr, perr)
			}
			bucket := &dto.Bucket{
				CumulativeCount: proto.Uint64(uint64(val)),
				UpperBound:      proto.Float64(le),
			}
			if exemplarOpt != nil {
				bucket.Exemplar = exemplarOpt
			}
			metric.Histogram.Bucket = append(metric.Histogram.Bucket, bucket)
		case suffixSum:
			metric.Histogram.SampleSum = proto.Float64(val)
		case suffixCount:
			metric.Histogram.SampleCount = proto.Uint64(uint64(val))
		}

	case dto.MetricType_SUMMARY:
		lp := stripStandardLabels(lbls, dto.MetricType_SUMMARY)
		metric := findOrCreateMetric(fam, a.metricIdx[familyName], lp, tsPtr)
		if metric.Summary == nil {
			metric.Summary = &dto.Summary{}
		}
		switch kind {
		case suffixSum:
			metric.Summary.SampleSum = proto.Float64(val)
		case suffixCount:
			metric.Summary.SampleCount = proto.Uint64(uint64(val))
		default:
			if q := lbls.Get("quantile"); q != "" {
				qv, perr := strconv.ParseFloat(q, 64)
				if perr != nil {
					return fmt.Errorf("invalid summary quantile=%q: %w", q, perr)
				}
				metric.Summary.Quantile = append(metric.Summary.Quantile, &dto.Quantile{
					Quantile: proto.Float64(qv),
					Value:    proto.Float64(val),
				})
			}
		}

	default:
		// UNTYPED / INFO / STATESET / GAUGE_HISTOGRAM and anything else: represent
		// each sample as an Untyped value. Info samples conventionally always have
		// value 1.0; Stateset samples are 0/1 per state. We preserve the value
		// verbatim so no information is lost.
		lp := stripStandardLabels(lbls, fam.GetType())
		metric := findOrCreateMetric(fam, a.metricIdx[familyName], lp, tsPtr)
		if metric.Untyped == nil {
			metric.Untyped = &dto.Untyped{}
		}
		metric.Untyped.Value = proto.Float64(val)
	}
	return nil
}

// suffix is the parsed family-suffix on the current series name.
type suffix int

const (
	suffixNone suffix = iota
	suffixTotal
	suffixCount
	suffixSum
	suffixBucket
	suffixCreated
)

// resolveFamily returns the parent family name and which standardized suffix the
// series uses (if any). It collapses _total / _count / _sum / _bucket / _created
// onto the parent family name iff a family with the matching type has been
// observed via a prior # TYPE entry. Otherwise the series stands on its own
// family.
func (a *omAssembler) resolveFamily(metricName string) (string, suffix) {
	for _, s := range []struct {
		suffix string
		kind   suffix
	}{
		{"_bucket", suffixBucket},
		{"_count", suffixCount},
		{"_sum", suffixSum},
		{"_total", suffixTotal},
		{"_created", suffixCreated},
	} {
		if strings.HasSuffix(metricName, s.suffix) && len(metricName) > len(s.suffix) {
			parent := metricName[:len(metricName)-len(s.suffix)]
			parentFam, parentTyped := a.families[parent]
			if parentTyped {
				switch s.kind {
				case suffixBucket:
					if parentFam.GetType() == dto.MetricType_HISTOGRAM {
						return parent, s.kind
					}
				case suffixCount, suffixSum:
					t := parentFam.GetType()
					if t == dto.MetricType_HISTOGRAM || t == dto.MetricType_SUMMARY {
						return parent, s.kind
					}
				case suffixTotal:
					if parentFam.GetType() == dto.MetricType_COUNTER {
						return parent, s.kind
					}
				case suffixCreated:
					return parent, s.kind
				}
			}
			// Suffix unambiguously implies a family even when # TYPE hasn't been
			// seen yet (OM is supposed to declare TYPE first, but be defensive).
			switch s.kind {
			case suffixBucket, suffixTotal, suffixCreated:
				return parent, s.kind
			}
		}
	}
	return metricName, suffixNone
}

// inferTypeFromSuffix is a fallback used when a series arrives before its # TYPE
// entry. OpenMetrics requires TYPE before any series, but we are defensive.
func inferTypeFromSuffix(k suffix) dto.MetricType {
	switch k {
	case suffixBucket:
		return dto.MetricType_HISTOGRAM
	case suffixSum, suffixCount:
		return dto.MetricType_HISTOGRAM // could be Summary; default Histogram
	case suffixTotal:
		return dto.MetricType_COUNTER
	}
	return dto.MetricType_UNTYPED
}

// stripStandardLabels drops the synthetic __name__ label and the type-specific
// dimensional labels ("le" for histograms, "quantile" for summaries) so they do
// not appear as user labels on the produced Metric.
func stripStandardLabels(lbls promLabels.Labels, mtype dto.MetricType) []*dto.LabelPair {
	out := make([]*dto.LabelPair, 0, lbls.Len())
	lbls.Range(func(lp promLabels.Label) {
		if lp.Name == commonModel.MetricNameLabel {
			return
		}
		if mtype == dto.MetricType_HISTOGRAM && lp.Name == "le" {
			return
		}
		if mtype == dto.MetricType_SUMMARY && lp.Name == "quantile" {
			return
		}
		out = append(out, &dto.LabelPair{
			Name:  proto.String(lp.Name),
			Value: proto.String(lp.Value),
		})
	})
	return out
}

// findOrCreateMetric returns an existing Metric with matching labels (so multiple
// per-bucket samples coalesce into one Metric per label set) or allocates a new
// one and appends it to the family.
//
// metricIdx maps a label-set hash key → index in fam.Metric so this stays
// O(1) per call even when a family has thousands of distinct label sets.
func findOrCreateMetric(fam *dto.MetricFamily, metricIdx map[string]int, labelPairs []*dto.LabelPair, tsPtr *int64) *dto.Metric {
	key := labelKey(labelPairs)
	if i, ok := metricIdx[key]; ok {
		return fam.Metric[i]
	}
	m := &dto.Metric{Label: labelPairs}
	if tsPtr != nil {
		m.TimestampMs = proto.Int64(*tsPtr)
	}
	metricIdx[key] = len(fam.Metric)
	fam.Metric = append(fam.Metric, m)
	return m
}

// labelKey serializes the label pairs into a compact string key for use as a
// map lookup. The order of incoming label pairs is stable (the parser sorts
// them via builder.Sort), so the resulting key is canonical per label set.
func labelKey(labelPairs []*dto.LabelPair) string {
	if len(labelPairs) == 0 {
		return ""
	}
	// Single-allocation join: pre-size buf to the exact required width.
	n := 0
	for _, lp := range labelPairs {
		n += len(lp.GetName()) + len(lp.GetValue()) + 2 // sep + sep
	}
	buf := make([]byte, 0, n)
	for _, lp := range labelPairs {
		buf = append(buf, lp.GetName()...)
		buf = append(buf, '\x00')
		buf = append(buf, lp.GetValue()...)
		buf = append(buf, '\x01')
	}
	return string(buf)
}

func toDTOExemplar(e *promExemplar.Exemplar) *dto.Exemplar {
	out := &dto.Exemplar{Value: proto.Float64(e.Value)}
	if e.HasTs {
		out.Timestamp = timestamppb.New(time.UnixMilli(e.Ts))
	}
	e.Labels.Range(func(lp promLabels.Label) {
		out.Label = append(out.Label, &dto.LabelPair{
			Name:  proto.String(lp.Name),
			Value: proto.String(lp.Value),
		})
	})
	return out
}

func toDTOMetricType(m commonModel.MetricType) dto.MetricType {
	switch m {
	case commonModel.MetricTypeCounter:
		return dto.MetricType_COUNTER
	case commonModel.MetricTypeGauge:
		return dto.MetricType_GAUGE
	case commonModel.MetricTypeHistogram:
		return dto.MetricType_HISTOGRAM
	case commonModel.MetricTypeGaugeHistogram:
		return dto.MetricType_GAUGE_HISTOGRAM
	case commonModel.MetricTypeSummary:
		return dto.MetricType_SUMMARY
	case commonModel.MetricTypeInfo, commonModel.MetricTypeStateset:
		// The dto model has no INFO or STATESET enum value; UNTYPED carries the
		// value verbatim.
		return dto.MetricType_UNTYPED
	}
	return dto.MetricType_UNTYPED
}
