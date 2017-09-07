package eureka

import "istio.io/pilot/model"

type serviceAccounts struct {
}

// NewServiceAccounts instantiates the Eureka service account interface
func NewServiceAccounts() model.ServiceAccounts {
	return &serviceAccounts{}
}

func (sa *serviceAccounts) GetIstioServiceAccounts(hostname string, ports []string) []string {
	return nil
}
