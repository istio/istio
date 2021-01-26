package staticvm

import (
	"fmt"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var _ echo.Workload = &workload{}

type workload struct {
	*client.Instance
	address string
}

func newWorkload(address string, grpcPort int) (*workload, error) {
	// TODO support TLS
	c, err := client.New(fmt.Sprintf("%s:%d", address, grpcPort), nil)
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

func (w *workload) LogsOrFail(t test.Failer) string {
	panic("implement me")
}
