package appoptics

import "net/http"

type MockMeasurementsService struct {
	OnCreate func([]*Measurement) (*http.Response, error)
}

func (m *MockMeasurementsService) Create(measurements []*Measurement) (*http.Response, error) {
	return m.OnCreate(measurements)
}
