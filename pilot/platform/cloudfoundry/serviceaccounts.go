package cloudfoundry

import "istio.io/istio/pilot/model"

type serviceAccounts struct {
}

func NewServiceAccounts() model.ServiceAccounts {
	return &serviceAccounts{}
}

func (sa *serviceAccounts) GetIstioServiceAccounts(hostname string, ports []string) []string {
	return nil
}
