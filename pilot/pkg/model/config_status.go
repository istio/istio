package model

import (
	"github.com/prometheus/client_golang/prometheus"
	"sync"
)

// config_status tracks configuration metrics and errors.
// Metrics are reset after a push - at the beginning all values are zero, and when push completes the status
// is reset.

var (

	// LastPushStatus preserves the metrics and data collected during lasts global push.
	// It can be used by debugging tools to inspect the push event. It will be reset after each push with the
	// new version.
	LastPushStatus *ConfigStatus
)

var (
	ipNotFound = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_no_ip",
		Help: "Pods not found in the endpoint table, possibly invalid.",
	})

	// TODO: use the cluster ID or parse the proxyID to determine the workload names and namespaces, for better reporting
	//ipNotFound = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	//	Name: "pilot_no_ip",
	//	Help: "Pods not found in the endpoint table, possibly invalid.",
	//}, []string{"workload"})

	unready = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_unready",
		Help: "Endpoints found in unready state",
	})

	conflictingOutbound = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_conf_out_listeners",
		Help: "Number of conflicting listeners.",
	})


)

func init() {
	prometheus.MustRegister(ipNotFound)
	prometheus.MustRegister(unready)
	prometheus.MustRegister(conflictingOutbound)

}

type ConfigStatus struct {
	Mutex sync.Mutex

	// ProxyWithoutService is keyed by the proxy ID, and holds proxies where no ServiceInstance was found.
	// This can be normal - for workloads that act only as client, or are not covered by a Service.
	// It can also be an error, for example in cases the Endpoint list of a service was not updated by the time
	// the sidecar calls.
	ProxyWithoutService map[string]*Proxy
	Unready map[string]*Proxy

	ConflictingOutbound map[string]string

}


// Tracked errors:
// - pilot_no_ip - set for sidecars without 'in' - either have no services or are not ready when the sidecar calls.

func GetStatus(obj interface{}) *ConfigStatus {
	env, ok := obj.(Environment)
	if !ok {
		return nil
	}
	return env.PushStatus
}

func NewStatus() *ConfigStatus {
	// TODO: detect push in progress, don't update status if set
	return &ConfigStatus{
		ProxyWithoutService: map[string]*Proxy{},
	}
}

func (cs *ConfigStatus) AfterPush() {
	cs.Mutex.Lock()
	defer cs.Mutex.Unlock()
	LastPushStatus = cs
	ipNotFound.Set(float64(len(cs.ProxyWithoutService)))
	unready.Set(float64(len(cs.Unready)))
	conflictingOutbound.Set(float64(len(cs.ConflictingOutbound)))

}