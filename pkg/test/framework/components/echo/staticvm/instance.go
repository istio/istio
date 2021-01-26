package staticvm

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"sync"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var _ echo.Instance = &instance{}

func init() {
	echo.RegisterFactory(cluster.StaticVM, newInstances)
}

type instance struct {
	id       resource.ID
	config   echo.Config
	address  string
	workload *workload
}

func newInstances(ctx resource.Context, config []echo.Config) (echo.Instances, error) {
	errG := multierror.Group{}
	mu := sync.Mutex{}
	var out echo.Instances
	for _, c := range config {
		c := c
		errG.Go(func() error {
			i, err := newInstance(ctx, c)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			out = append(out, i)
			return nil
		})
	}
	if err := errG.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func newInstance(ctx resource.Context, config echo.Config) (echo.Instance, error) {
	var grpcPort echo.Port
	for _, port := range config.Ports {
		if port.Protocol == protocol.GRPC {
			grpcPort = port
		}
	}
	if grpcPort.Protocol != protocol.GRPC {
		return nil, fmt.Errorf("no grpc port for %s", config.Service)
	}
	workload, err := newWorkload(config.StaticAddress, grpcPort.InstancePort)
	if err != nil {
		return nil, err
	}
	i := &instance{
		config:   config,
		address:  config.StaticAddress,
		workload: workload,
	}
	i.id = ctx.TrackResource(i)
	return i, nil
}

func (i *instance) ID() resource.ID {
	return i.id
}

func (i *instance) Config() echo.Config {
	return i.config
}

func (i *instance) Address() string {
	return i.address
}

func (i *instance) Workloads() ([]echo.Workload, error) {
	return []echo.Workload{i.workload}, nil
}

func (i *instance) WorkloadsOrFail(t test.Failer) []echo.Workload {
	w, err := i.Workloads()
	if err != nil {
		t.Fatalf("failed getting workloads for %s", i.Config().Service)
	}
	return w
}

func (i *instance) Call(opts echo.CallOptions) (client.ParsedResponses, error) {
	return common.ForwardEcho(i.Config().Service, i.workload.Instance, &opts, false)
}

func (i *instance) CallOrFail(t test.Failer, opts echo.CallOptions) client.ParsedResponses {
	t.Helper()
	res, err := i.Call(opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (i *instance) CallWithRetry(opts echo.CallOptions, retryOptions ...retry.Option) (client.ParsedResponses, error) {
	return common.ForwardEcho(i.Config().Service, i.workload.Instance, &opts, true, retryOptions...)
}

func (i *instance) CallWithRetryOrFail(t test.Failer, opts echo.CallOptions, retryOptions ...retry.Option) client.ParsedResponses {
	t.Helper()
	res, err := i.CallWithRetry(opts, retryOptions...)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (i *instance) Restart() error {
	panic("implement me")
}
