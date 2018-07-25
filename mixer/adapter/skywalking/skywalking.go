//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/skywalking/config/config.proto
package skywalking

import (
	"context"

	"istio.io/istio/mixer/adapter/skywalking/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
	"google.golang.org/grpc"
	pb "istio.io/istio/mixer/adapter/skywalking/protocol"
	"time"
)

type (
	builder struct {
		adpCfg      *config.Params
		metricTypes map[string]*metric.Type
	}
	handler struct {
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

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial(b.adpCfg.ServerAddress, opts...)

	if err != nil {
		env.Logger().Errorf("fail to dial: %v", err)
	}

	c := pb.NewServiceMeshMetricServiceClient(conn)

	return &handler{metricTypes: b.metricTypes, env: env, client: c}, err

}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	// Check if the path is valid
	return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	h.env.Logger().Debugf("Begin to create client")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientStream, err := h.client.Collect(ctx)
	if err != nil {
		h.env.Logger().Errorf("%v.Collect(_) = _, %v", h.client, err)
	}

	for _, inst := range insts {
		if _, ok := h.metricTypes[inst.Name]; !ok {
			h.env.Logger().Errorf("Cannot find Type for instance %s", inst.Name)
			continue
		}

		time.Now()
		requestMethod := inst.Dimensions["requestMethod"].(string)
		requestPath := inst.Dimensions["requestPath"].(string)
		requestScheme := inst.Dimensions["requestScheme"].(string)
		requestTime := inst.Dimensions["requestTime"].(time.Time)
		responseTime := inst.Dimensions["responseTime"].(time.Time)
		responseCode := inst.Dimensions["responseCode"].(int64)
		reporter := inst.Dimensions["reporter"]
		protocol := inst.Dimensions["apiProtocol"].(string)

		var endpoint string
		var status bool
		var netProtocol pb.Protocol
		if protocol == "http" || protocol == "https" {
			endpoint = requestScheme + "/" + requestMethod + "/" + requestPath
			status = responseCode >= 200 && responseCode < 400
			netProtocol = pb.Protocol_HTTP;
		} else {
			//grpc
			endpoint = protocol + "/" + requestPath
			netProtocol = pb.Protocol_gRPC;
		}

		latency := int32(responseTime.Sub(requestTime).Nanoseconds() / int64(time.Millisecond));
		var detectPoint pb.DetectPoint
		if "source" == reporter {
			detectPoint = pb.DetectPoint_client
		} else {
			detectPoint = pb.DetectPoint_server
		}

		var metric = pb.ServiceMeshMetric{
			StartTime:             requestTime.UnixNano() / int64(time.Millisecond),
			EndTime:               responseTime.UnixNano() / int64(time.Millisecond),
			SourceServiceName:     inst.Dimensions["sourceService"].(string),
			SourceServiceInstance: inst.Dimensions["sourceUID"].(string),
			DestServiceName:       inst.Dimensions["destinationService"].(string),
			DestServiceInstance:   inst.Dimensions["destinationUID"].(string),
			Endpoint:              endpoint,
			Latency:               latency,
			ResponseCode:          int32(responseCode),
			Status:                status,
			Protocol:              netProtocol,
			DetectPoint:           detectPoint,
		}

		clientStream.Send(&metric)
	}

	// Not downstream
	_, recErr := clientStream.CloseAndRecv()
	if err != nil {
		h.env.Logger().Errorf("%v.CloseAndRecv() got error %v, want %v", clientStream, recErr, nil)
	}


	return nil
}

// adapter.Handler#Close
func (h *handler) Close() error {
	return nil
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
