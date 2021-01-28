package staticvm

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var _ echo.Workload = &workload{}

type workload struct {
	*client.Instance
	address string
}

func newWorkloads(address []string, grpcPort int, tls *common.TLSSettings) ([]echo.Workload, error) {
	var errs error
	var out []echo.Workload
	for _, ip := range address {
		w, err := newWorkload(ip, grpcPort, tls)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		out = append(out, w)
	}
	if errs != nil {
		return nil, errs
	}
	return out, nil
}

func newWorkload(address string, grpcPort int, tls *common.TLSSettings) (*workload, error) {
	c, err := client.New(fmt.Sprintf("%s:%d", address, grpcPort), tls)
	if err != nil {
		return nil, err
	}
	return &workload{
		Instance: c,
	}, nil
}

func (w *workload) PodName() string {
	return ""
}

func (w *workload) Address() string {
	return w.address
}

func (w *workload) Sidecar() echo.Sidecar {
	panic("implement me")
}

func (w *workload) Logs() (string, error) {
	panic("implement me")
}

func (w *workload) LogsOrFail(_ test.Failer) string {
	panic("implement me")
}
