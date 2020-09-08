package istioagent

type Prober interface {
	// Probe will healthcheck and return whether or not the target is healthy.
	Probe() (bool, error)
}

type HTTPProber struct {
	// api HTTPProbe protobuf msg
}

func (h *HTTPProber) Probe() (bool, error) {
	return false, nil
}

type TCPProber struct {
	// api TCPProbe protobuf msg
}

func (t *TCPProber) Probe() (bool, error) {
	return false, nil
}

type ExecProber struct {
	// api ExecProbe protobuf msg
}

func (e *ExecProber) Probe() (bool, error) {
	return false, nil
}