// Copyright 2017 Istio Authors.
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

package descriptor

import (
	"fmt"
	"math"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/adapter"
)

// ValidateLogEntry validates a log entry descriptor.
func ValidateLogEntry(desc *dpb.LogEntryDescriptor) (ce *adapter.ConfigErrors) {
	if desc.PayloadFormat == dpb.PAYLOAD_FORMAT_UNSPECIFIED {
		ce = ce.Appendf("payloadFormat", "a payload format must be provided, we got PAYLOAD_FORMAT_UNSPECIFIED")
	}
	if desc.LogTemplate == "" {
		ce = ce.Appendf("logTemplate", "a log template must be provided")
	}
	ce = ce.Extend(validateLabels(desc.Labels))
	ce = ce.Extend(validateDescriptorName(desc.Name))
	return
}

// ValidateMetric validates a metric descriptor.
func ValidateMetric(desc *dpb.MetricDescriptor) (ce *adapter.ConfigErrors) {
	if desc.Kind == dpb.METRIC_KIND_UNSPECIFIED {
		ce = ce.Appendf("kind", "a metric kind must be provided, we got METRIC_KIND_UNSPECIFIED")
	}
	if desc.Value == dpb.VALUE_TYPE_UNSPECIFIED {
		ce = ce.Appendf("value", "a type must be provided for the metric's value, we got VALUE_TYPE_UNSPECIFIED")
	}
	// TODO: it'd be nice to be able to log that a user gave us buckets but didn't specify kind: DISTRIBUTION.
	// It's not an error, since they're giving us extra info, so failing seems bad, but it could indicate a misconfiguration.
	if desc.Kind == dpb.DISTRIBUTION {
		ce = ce.Extend(validateBucket(desc.Buckets))
	}
	ce = ce.Extend(validateLabels(desc.Labels))
	ce = ce.Extend(validateDescriptorName(desc.Name))
	return
}

func validateBucket(desc *dpb.MetricDescriptor_BucketsDefinition) (ce *adapter.ConfigErrors) {
	if buckets := desc.GetLinearBuckets(); buckets != nil {
		return validateLinearBuckets(buckets)
	} else if buckets := desc.GetExplicitBuckets(); buckets != nil {
		return validateExplicitBuckets(buckets)
	} else if buckets := desc.GetExponentialBuckets(); buckets != nil {
		return validateExponentialBuckets(buckets)
	}
	return ce.Appendf("buckets", "unknown bucket type: %+v", desc)
}

func validateLinearBuckets(linear *dpb.MetricDescriptor_BucketsDefinition_Linear) (ce *adapter.ConfigErrors) {
	if linear.NumFiniteBuckets <= 0 {
		ce = ce.Appendf("buckets.linearBuckets.numFiniteBuckets", "must be greater than 0, got %d", linear.NumFiniteBuckets)
	}
	if linear.Width <= 0 {
		ce = ce.Appendf("buckets.linearBuckets.width", "must be greater than 0, got %d", linear.Width)
	}
	return
}

func validateExplicitBuckets(explicit *dpb.MetricDescriptor_BucketsDefinition_Explicit) (ce *adapter.ConfigErrors) {
	prevBound := -math.MaxFloat64
	for idx, bound := range explicit.Bounds {
		if math.Max(prevBound, bound) == prevBound || almostEq(prevBound, bound, 5) {
			ce = ce.Appendf("buckets.explicitBuckets.bounds",
				"bounds must be strictly increasing, buckets %d and %d have values %f and %f respectively", idx-1, idx, prevBound, bound)
		} else {
			prevBound = bound
		}
	}
	return
}

func validateExponentialBuckets(exponential *dpb.MetricDescriptor_BucketsDefinition_Exponential) (ce *adapter.ConfigErrors) {
	if exponential.NumFiniteBuckets <= 0 {
		ce = ce.Appendf("buckets.exponentialBuckets.numFiniteBuckets", "must be greater than 0, got %d", exponential.NumFiniteBuckets)
	}
	if exponential.GrowthFactor <= 1 {
		ce = ce.Appendf("buckets.exponentialBuckets.growthFactor", "must be greater than 1, got %d", exponential.GrowthFactor)
	}
	if exponential.Scale <= 0 {
		ce = ce.Appendf("buckets.exponentialBuckets.scale", "must be greater than 0, got %d", exponential.Scale)
	}
	return
}

// ValidateQuota validates a quota descriptor.
func ValidateQuota(desc *dpb.QuotaDescriptor) (ce *adapter.ConfigErrors) {
	ce = ce.Extend(validateLabels(desc.Labels))
	ce = ce.Extend(validateDescriptorName(desc.Name))
	return
}

// ValidateMonitoredResource validates a monitored resource descriptor.
func ValidateMonitoredResource(desc *dpb.MonitoredResourceDescriptor) (ce *adapter.ConfigErrors) {
	ce = ce.Extend(validateLabels(desc.Labels))
	ce = ce.Extend(validateDescriptorName(desc.Name))
	return
}

// ValidatePrincipal validates a principal descriptor.
func ValidatePrincipal(desc *dpb.PrincipalDescriptor) (ce *adapter.ConfigErrors) {
	ce = ce.Extend(validateLabels(desc.Labels))
	ce = ce.Extend(validateDescriptorName(desc.Name))
	return
}

func validateDescriptorName(name string) (ce *adapter.ConfigErrors) {
	if name == "" {
		ce = ce.Appendf("name", "a descriptor's name must not be empty")
	}
	return
}

func validateLabels(labels map[string]dpb.ValueType) (ce *adapter.ConfigErrors) {
	for name, vt := range labels {
		if vt == dpb.VALUE_TYPE_UNSPECIFIED {
			ce = ce.Appendf(fmt.Sprintf("labels[%s]", name), "must have a value type, we got VALUE_TYPE_UNSPECIFIED")
		}
	}
	return
}

// Returns true iff there are less than or equal to 'steps' representable floating point values between a and b. Steps == 0 means
// two exactly equal floats; in practice allowing a few steps between floats is fine for equality. Typically if A is one
// step larger than B, A <= 1.000000119 * B (barring a few classes of exceptions).
//
// See https://randomascii.wordpress.com/2012/02/25/comparing-floating-point-numbers-2012-edition/ for more details.
func almostEq(a, b float64, steps int64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	} else if math.Signbit(a) != math.Signbit(b) {
		// They're different, but +0 == -0, so we check to be sure.
		return a == b
	}
	// When we interpret a floating point number's bits as an integer, the difference between two floats-as-ints
	// is one plus the number of representable floating point values between the two. This is called "Units in the
	// Last Place", or "ULPs".
	ulps := int64(math.Float64bits(a)) - int64(math.Float64bits(b))
	// Go's math package only supports abs on float64, we don't want to round-trip through float64 again, so we do it ourselves.
	if ulps < 0 {
		ulps = -ulps
	}
	return ulps <= steps
}
