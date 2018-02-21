/* Package metrics implements a facade over low level metrics sources to put into a format suitable for debug log
instrumentation or streamz.
Example:
  m := NewMetrics(MemoryMetric{}, CPUMetric{OneSecAvg: true})
  log.Debugf("Field names: %s", m.FieldNames())
  log.Debugf("Compact format: %s", m)
  log.Debugf("Full format: %s", m.DebugString())
  log.Debugf("Raw data: %s", pretty.Sprint(m.Value()))

Output:
  Field names: AllocMib/SysMiB/NumGC, OneSecAvg (average over all CPUs)
  Compact format: 10.0/20.0/5, 5
  Full format: 10.0/20.0/5 AllocMib/SysMiB/NumGC, 5 OneSecAvg (average over all CPUs)
  Raw data: { "Memory": {
                "AllocMib": [10.0], "SysMiB": [20.0], "NumGC": [5]
			  },
			  "CPU": {
				"OneSecAvg": [5]
			  } }
 */
package metrics

import (
	"github.com/shirou/gopsutil/cpu"
	"runtime"
	"strings"
	"time"
	"k8s.io/kubernetes/bazel-kubernetes/external/go_sdk/src/fmt"
)

type Metric interface {
	Name() string
	FieldNames() string
	String() string
	DebugString() string
	Value() map[string][]float64
}

type Metrics struct {
	metrics []Metric
}

func NewMetrics(metrics ...Metric) Metrics {
	out := Metrics{}
	out.metrics = append(out.metrics, metrics...)
	return out
}

func (m Metrics) FieldNames() string {
	var out []string
	for _, mm := range m.metrics {
		out = append(out, mm.FieldNames())
	}
	return strings.Join(out, ",")
}

func (m Metrics) String() string {
	var out []string
	for _, mm := range m.metrics {
		out = append(out, mm.String())
	}
	return strings.Join(out, ",")
}

func (m Metrics) DebugString() string {
	var out []string
	for _, mm := range m.metrics {
		out = append(out, mm.DebugString())
	}
	return strings.Join(out, ",")
}

type MemoryMetric struct{}

func (m MemoryMetric) Description() string {
	return "AllocMib/SysMiB/NumGC"
}

func (m MemoryMetric) String() string {
	alloc, sys, gc := memUsage()
	return fmt.Sprintf("%.1f,/.1f/%d", alloc, sys, gc)
}

func (m MemoryMetric) DebugString() string {
	return m.String() + " " + m.Description()
}

func (m MemoryMetric) Value() map[string][]float64 {
	alloc, sys, gc := memUsage()
	return map[string][]float64{
		"AllocMib": {alloc},
		"SysMiB":   {sys},
		"NumGC":    {float64(gc)},
	}
}

type CPUMetric struct {
	OneSecAvg   bool
	FiveSecAvg  bool
	OneMinAvg   bool
	FiveMinAvg  bool
	PerCPU      bool
	SummaryType CPUSummaryMetricType
}

type CPUSummaryMetricType int

const (
	CPUMetricAverage CPUSummaryMetricType = iota
	CPUMetricMax
)

func (c CPUMetric) Description() string {
	var out []string
	switch {
	case c.OneSecAvg:
		out = append(out, "OneSecAvg")
	case c.FiveSecAvg:
		out = append(out, "FiveSecAvg")
	case c.OneMinAvg:
		out = append(out, "OneMinAvg")
	case c.FiveMinAvg:
		out = append(out, "FiveMinAvg")
	}

	outStr := strings.Join(out, "/")

	if c.PerCPU {
		outStr += " (each metric shown as per CPU array)"
	} else {
		switch c.SummaryType {
		case CPUMetricAverage:
			outStr += " (average over all CPUs)"
		case CPUMetricMax:
			outStr += " (max from all CPUs)"
		}
	}

	return outStr
}

func (c CPUMetric) String() string {
	var out []string
	switch {
	case c.OneSecAvg:
		out = append(out, getCPUPctString(time.Second, c.PerCPU, c.SummaryType))
	case c.FiveSecAvg:
		out = append(out, getCPUPctString(5*time.Second, c.PerCPU, c.SummaryType))
	case c.OneMinAvg:
		out = append(out, getCPUPctString(time.Minute, c.PerCPU, c.SummaryType))
	case c.FiveMinAvg:
		out = append(out, getCPUPctString(5*time.Minute, c.PerCPU, c.SummaryType))
	}

	return "CPU:" + strings.Join(out, ",")
}

func (c CPUMetric) DebugString() string {
	return c.String() + " " + c.Description()
}

func (c CPUMetric) Value() map[string][]float64 {
	out := make(map[string][]float64)
	switch {
	case c.OneSecAvg:
		out["OneSecAvg"] = getCPUPct(time.Second, c.PerCPU, c.SummaryType)
	case c.FiveSecAvg:
		out["FiveSecAvg"] = getCPUPct(5*time.Second, c.PerCPU, c.SummaryType)
	case c.OneMinAvg:
		out["OneMinAvg"] = getCPUPct(time.Minute, c.PerCPU, c.SummaryType)
	case c.FiveMinAvg:
		out["FiveMinAvg"] = getCPUPct(5*time.Minute, c.PerCPU, c.SummaryType)
	}
	return out
}

func getCPUPct(interval time.Duration, perCPU bool, summaryType CPUSummaryMetricType) []float64 {
	cpupct, err := cpu.Percent(interval, true)
	if err != nil {
		return []float64{-1}
	}
	if perCPU {
		return cpupct
	}
	switch summaryType {
	case CPUMetricAverage:
		return []float64{avgOfFloatSlice(cpupct)}
	case CPUMetricMax:
		return []float64{maxOfFloatSlice(cpupct)}
	}
	return []float64{-1}
}

func getCPUPctString(interval time.Duration, perCPU bool, summaryType CPUSummaryMetricType) string {
	cpupct, err := cpu.Percent(interval, true)
	if err != nil {
		return fmt.Sprint(err)
	}
	cpuv := floatToInt(cpupct)

	if perCPU {
		return fmt.Sprint(cpuv)
	}

	switch summaryType {
	case CPUMetricAverage:
		return fmt.Sprint(avgOfIntSlice(cpuv))
	case CPUMetricMax:
		return fmt.Sprint(maxOfIntSlice(cpuv))
	}

	return "unknown CPUSummaryMetricType"
}

func memUsage() (float64, float64, uint32) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return bToMiB(m.Alloc), bToMiB(m.Sys), m.NumGC
}

func avgOfIntSlice(iv []int64) int64 {
	return int64(avgOfFloatSlice(intToFloat(iv)))
}

func maxOfIntSlice(iv []int64) int64 {
	return int64(maxOfFloatSlice(intToFloat(iv)))
}

func avgOfFloatSlice(iv []float64) float64 {
	if len(iv) == 0 {
		return -1
	}
	var tot float64

	for _, v := range iv {
		tot += float64(v)
	}
	return tot / float64(len(iv))
}

func maxOfFloatSlice(iv []float64) float64 {
	if len(iv) == 0 {
		return -1
	}
	max := iv[0]

	for _, v := range iv {
		if v > max {
			max = v
		}
	}
	return max
}

func floatToInt(f []float64) []int64 {
	var out []int64
	for _, v := range f {
		out = append(out, int64(v))
	}
	return out
}

func intToFloat(f []int64) []float64 {
	var out []float64
	for _, v := range f {
		out = append(out, float64(v))
	}
	return out
}

func bToMiB(b uint64) float64 {
	return float64(b) / 1024.0 / 1024.0
}
