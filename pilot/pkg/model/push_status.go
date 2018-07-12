package model

import (
	"sync"

	"time"

	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pkg/log"
	"encoding/json"
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
	ProxyStatus map[string]map[string]PushStatusEvent

	// Start represents the time of last config change that reset the
	// push status.
	Start time.Time

	End time.Time
}

type PushStatusEvent struct {
	Proxy   string `json:"proxy,omitempty"`
	Message string `json:"message,omitempty"`
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
		metricMap = map[string]PushStatusEvent{}
		ps.ProxyStatus[metric.Name] = metricMap
	}
	ev := PushStatusEvent{Message: msg}
	if proxy != nil {
		ev.Proxy = proxy.ID
	}
	metricMap[key] = ev
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
	// Updated by GetProxyServiceInstances. Normal condition when starting
	// an app with readiness, error if it doesn't change to 0.
	METRIC_ENDPOINT_NOT_READY = newPushMetric(
		"pilot_endpoint_not_ready",
		"Endpoint found in unready state.",
	)

	// METRIC_CONFLICTING_HTTP_OUTBOUND tracks cases of multiple outbound
	// listeners, with accepted HTTP and the conflicting one a
	// different type
	METRIC_CONFLICTING_HTTP_OUTBOUND = newPushMetric(
		"pilot_conf_out_http_listeners",
		"Number of conflicting listeners on a http port.",
	)

	// METRIC_CONFLICTING_TCP_OUTBOUND tracks cases of multiple outbound
	// listeners, with accepted TCP and the conflicting one a
	// different type
	METRIC_CONFLICTING_TCP_OUTBOUND = newPushMetric(
		"pilot_conf_out_tcp_listeners",
		"Number of conflicting listeners on a tcp listener.",
	)

	// METRIC_CONFLICTING_INBOUND tracks cases of multiple inbound
	// listeners - 2 services selecting the same port of the pod
	METRIC_CONFLICTING_INBOUND = newPushMetric(
		"pilot_conf_in_listeners",
		"Number of conflicting inbound listeners.",
	)

	// METRIC_NO_INSTANCES tracks clusters (services) without workloads.
	METRIC_NO_INSTANCES = newPushMetric(
		"pilot_eds_no_instances",
		"Number of clusters without instances.",
	)

	// LastPushStatus preserves the metrics and data collected during lasts global push.
	// It can be used by debugging tools to inspect the push event. It will be reset after each push with the
	// new version.
	LastPushStatus *PushStatus

	// All metrics we registered.
	metrics []*PushMetric
)

// NewStatus creates a new PushStatus structure to track push status.
func NewStatus() *PushStatus {
	// TODO: detect push in progress, don't update status if set
	return &PushStatus{
		ProxyStatus: map[string]map[string]PushStatusEvent{},
		Start:       time.Now(),
	}
}

// MarshalJSON implements json.Marshaller, with a lock.
func (cs *PushStatus) JSON() ([]byte, error) {
	if cs == nil {
		return []byte{'{','}'}, nil
	}
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return json.MarshalIndent(cs, "", "    ")
}


// OnConfigChange is called when a config change is detected.
func (cs *PushStatus) OnConfigChange() {
	LastPushStatus = cs
	cs.UpdateMetrics()
}

func (cs *PushStatus) UpdateMetrics() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	for _, pm := range metrics {
		mmap, f := cs.ProxyStatus[pm.Name]
		if f {
			pm.gauge.Set(float64(len(mmap)))
		} else {
			pm.gauge.Set(0)
		}
	}
}
