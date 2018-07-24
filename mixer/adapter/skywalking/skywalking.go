//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/skywalking/config/config.proto
package skywalking

import (
	"context"

	"fmt"
	"os"
	"path/filepath"
	"istio.io/istio/mixer/adapter/skywalking/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
	"google.golang.org/grpc"
	pb "istio.io/istio/mixer/adapter/skywalking/protocol"
	"time"
	"io"
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
		client      pb.ServiceMeshMetricServiceClient
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

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial(b.adpCfg.ServerAddress, opts...)

	if err != nil {
		env.Logger().Errorf("fail to dial: %v", err)
	}

	c := pb.NewServiceMeshMetricServiceClient(conn)

	return &handler{f: file, metricTypes: b.metricTypes, env: env, client: c}, err

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
	var clientStream pb.ServiceMeshMetricService_CollectClient
	waitc := make(chan struct{})

	for _, inst := range insts {
		if _, ok := h.metricTypes[inst.Name]; !ok {
			h.env.Logger().Errorf("Cannot find Type for instance %s", inst.Name)
			continue
		}
		if (clientStream == nil) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			clientStream, err := h.client.Collect(ctx)
			if err != nil {
				h.env.Logger().Errorf("%v.Collect(_) = _, %v", h.client, err)
			}

			go func() {
				for {
					// Don't have any downstream(_) right now.
					_, err := clientStream.CloseAndRecv()
					if err == io.EOF {
						// read done.
						close(waitc)
						return
					}
					if err != nil {
						h.env.Logger().Errorf("Failed to receive the downstream : %v", err)
					}
				}
			}()
		}

		requestMethod := inst.Dimensions["requestMethod"].(string)
		requestPath := inst.Dimensions["requestPath"].(string)
		requestScheme := inst.Dimensions["requestScheme"].(string)
		requestTime := inst.Dimensions["requestTime"].(int64)
		responseTime := inst.Dimensions["responseTime"].(int64)
		responseCode := inst.Dimensions["responseCode"].(int32)
		reporter := inst.Dimensions["reporter"]
		protocol := inst.Dimensions["apiProtocol"].(string)

		var endpoint string
		var status bool
		var netProtocol pb.Protocol
		if (protocol == "http" || protocol == "https") {
			endpoint = requestScheme + "/" + requestMethod + "/" + requestPath
			status = responseCode >= 200 && responseCode < 400
			netProtocol = pb.Protocol_HTTP;
		} else {
			//grpc
			endpoint = protocol + "/" + requestPath
			netProtocol = pb.Protocol_gRPC;
		}
		latency := int32(responseTime - requestTime)

		var detectPoint pb.DetectPoint
		if (reporter == "source") {
			detectPoint = pb.DetectPoint_client
		} else {
			detectPoint = pb.DetectPoint_server
		}

		var metric = pb.ServiceMeshMetric{
			SourceServiceName:     inst.Dimensions["sourceService"].(string),
			SourceServiceInstance: inst.Dimensions["sourceUID"].(string),
			DestServiceName:       inst.Dimensions["destinationService"].(string),
			DestServiceInstance:   inst.Dimensions["destinationUID"].(string),
			Endpoint:              endpoint,
			Latency:               latency,
			ResponseCode:          responseCode,
			Status:                status,
			Protocol:              netProtocol,
			DetectPoint:           detectPoint,
		}

		clientStream.Send(&metric)

		h.f.WriteString(fmt.Sprintf(`requestMethod: '%s'`, requestMethod))
		h.f.WriteString(fmt.Sprintf(`requestPath: '%s'`, requestPath))
		h.f.WriteString(fmt.Sprintf(`requestScheme: '%s'`, requestScheme))
		h.f.WriteString(fmt.Sprintf(`requestTime: '%s'`, requestTime))
		h.f.WriteString(fmt.Sprintf(`responseTime: '%s'`, responseTime))
		h.f.WriteString(fmt.Sprintf(`responseCode: '%s'`, responseCode))
		h.f.WriteString(fmt.Sprintf(`reporter: '%s'`, reporter))

		// debug only
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

	if (clientStream != nil) {
		clientStream.CloseSend()
		<-waitc
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
