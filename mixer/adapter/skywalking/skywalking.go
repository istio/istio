//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/skywalking/config/config.proto
package skywalking

import (
	"context"

	// "github.com/gogo/protobuf/types"
	"fmt"
	"os"
	"path/filepath"
	"istio.io/istio/mixer/adapter/skywalking/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		adpCfg      *config.Params
		metricTypes map[string]*metric.Type
	}
	handler struct {
		f           *os.File
		metricTypes map[string]*metric.Type
		env         adapter.Env
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	var err error
	var file *os.File
	file, err = os.Create(b.adpCfg.FilePath)
	return &handler{f: file, metricTypes: b.metricTypes, env: env}, err

}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	// Check if the path is valid
	if _, err := filepath.Abs(b.adpCfg.FilePath); err != nil {
		ce = ce.Append("file_path", err)
	}
	return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	for _, inst := range insts {
		h.f.WriteString(`output:`)
		if _, ok := h.metricTypes[inst.Name]; !ok {
			h.env.Logger().Errorf("Cannot find Type for instance %s", inst.Name)
			continue
		}
		h.f.WriteString(fmt.Sprintf(`HandleMetric invoke for :
		Instance Name  :'%s'
		Instance Value : %v,
		Type           : %v`, inst.Name, *inst, *h.metricTypes[inst.Name]))
	}



	return nil
}

// adapter.Handler#Close
func (h *handler) Close() error {
	return h.f.Close()
}

////////////////// Bootstrap //////////////////////////
// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "skywalking",
		Description: "Collect the traffic meta info and report to backend",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{},
	}
}
