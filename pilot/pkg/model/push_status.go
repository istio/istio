package model

import (
	"sync"

	"time"

	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pkg/log"
)

// PushStatus tracks the status of a mush - metrics and errors.
// Metrics are reset after a push - at the beginning all
// values are zero, and when push completes the status is reset.
// The struct is exposed in a debug endpoint - fields public to allow
// easy serialization as json.
type PushStatus struct {
	mutex sync.Mutex `json:"-"`

	// ProxyStatus is keyed by the error code, and holds a map keyed
	// by the ID.
	ProxyStatus map[string]map[string]*Proxy

	Start time.Time
}

type PushMetric struct {
	Name  string
	gauge prometheus.Gauge
}

func newPushMetric(name, help string) *PushMetric {
	pm := &PushMetric{
		gauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: name,
			Help: help,
		}),
		Name: name,
	}
	prometheus.MustRegister(pm.gauge)
	metrics = append(metrics, pm)
	return pm
}

// Add will add an case to the metric.
func (ps *PushStatus) Add(metric *PushMetric, key string, proxy *Proxy, msg string) {
	if ps == nil {
		log.Infof("Metric without context %s %v %s", key, proxy, msg)
		return
	}
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	metricMap, f := ps.ProxyStatus[metric.Name]
	if !f {
		metricMap = map[string]*Proxy{}
		ps.ProxyStatus[metric.Name] = metricMap
	}
	metricMap[key] = proxy
}

var (
	// METRIC_PROXY_NO_SERVICE represents proxies not selected by any service
	// This can be normal - for workloads that act only as client, or are not covered by a Service.
	// It can also be an error, for example in cases the Endpoint list of a service was not updated by the time
	// the sidecar calls.
	// Updated by GetProxyServiceInstances
	METRIC_PROXY_NO_SERVICE = newPushMetric(
		"pilot_no_ip",
		"Pods not found in the endpoint table, possibly invalid.",
	)

	// METRIC_PROXY_UNREADY represents proxies found not be ready.
	// Updated by GetProxyServiceInstances
	METRIC_PROXY_UNREADY = newPushMetric(
		"pilot_unready",
		"Endpoints found in unready state.",
	)

	METRIC_CONFLICTING_OUTBOUND = newPushMetric(
		"pilot_conf_out_listeners",
		"Number of conflicting listeners.",
	)

	METRIC_NO_INSTANCES = newPushMetric(
		"pilot_eds_no_instances",
		"Number of clusters without instances.",
	)

	// LastPushStatus preserves the metrics and data collected during lasts global push.
	// It can be used by debugging tools to inspect the push event. It will be reset after each push with the
	// new version.
	LastPushStatus *PushStatus
)

var (

	// All metrics we registered.
	metrics []*PushMetric
)

// NewStatus creates a new PushStatus structure to track push status.
func NewStatus() *PushStatus {
	// TODO: detect push in progress, don't update status if set
	return &PushStatus{
		ProxyStatus: map[string]map[string]*Proxy{},
		Start:       time.Now(),
	}
}

// AfterPush is called after a push to update the gauges and the debug
// status.
func (cs *PushStatus) AfterPush() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	LastPushStatus = cs

	for _, pm := range metrics {
		mmap, f := cs.ProxyStatus[pm.Name]
		if f {
			pm.gauge.Set(float64(len(mmap)))
		} else {
			pm.gauge.Set(0)
		}
	}
}
