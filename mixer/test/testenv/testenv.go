package testenv

import (
	"io"
	"net"

	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/cmd/server/cmd"
	"istio.io/mixer/cmd/shared"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/template"
)

// TestEnv interface
type TestEnv interface {
	io.Closer
	CreateMixerClient() (mixerpb.MixerClient, *grpc.ClientConn, error)
}

type testEnv struct {
	addr         string
	mixerContext *cmd.ServerContext
	shutdown     chan struct{}
}

// Args includes the required args to initialize Mixer server.
type Args struct {
	MixerServerAddr               string
	ConfigStoreURL                string
	ConfigStore2URL               string
	ConfigDefaultNamespace        string
	ConfigIdentityAttribute       string
	ConfigIdentityAttributeDomain string
	UseAstEvaluator               bool
}

// NewEnv creates a TestEnv instance.
func NewEnv(args *Args, info map[string]template.Info, adapters []adapter.InfoFn) (TestEnv, error) {
	lis, err := net.Listen("tcp", args.MixerServerAddr)
	if err != nil {
		return nil, err
	}

	context := cmd.SetupTestServer(info, adapters, []adapter.RegisterFn{}, args.ConfigStoreURL, args.ConfigStore2URL,
		args.ConfigDefaultNamespace, args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain, args.UseAstEvaluator)
	shutdown := make(chan struct{})

	go func() {
		shared.Printf("Start test Mixer on:%v\n", lis.Addr())
		{
			defer context.GP.Close()
			defer context.AdapterGP.Close()
			if err := context.Server.Serve(lis); err != nil {
				shared.Printf("Mixer Shutdown: %v\n", err)
			}
		}
		shutdown <- struct{}{}
	}()

	return &testEnv{
		addr:         lis.Addr().String(),
		mixerContext: context,
		shutdown:     shutdown,
	}, nil
}

// CreateMixerClient returns a Mixer Grpc client.
func (env *testEnv) CreateMixerClient() (mixerpb.MixerClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(env.addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return mixerpb.NewMixerClient(conn), conn, nil
}

// Close cleans up TestEnv.
func (env *testEnv) Close() error {
	env.mixerContext.Server.GracefulStop()
	<-env.shutdown
	close(env.shutdown)
	env.mixerContext = nil
	return nil
}

// GetAttrBag creates Attributes proto.
func GetAttrBag(attrs map[string]interface{}, identityAttr, identityAttrDomain string) mixerpb.Attributes {
	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(identityAttr, identityAttrDomain)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto mixerpb.Attributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}
