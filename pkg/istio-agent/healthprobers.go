package istioagent

type Prober interface {
	// Probe will healthcheck and return whether or not the target is healthy.
	Probe() (bool, error)
}

type HTTPProber struct {
}

func (h *HTTPProber) Probe() (bool, error) {
	return false, nil
}

type TCPProber struct {
}

func (t *TCPProber) Probe() (bool, error) {

}

type ExecProber struct {
}

func (e *ExecProber) Probe() (bool, error) {

}