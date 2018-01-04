package appoptics

import (
	"context"
	"testing"
	"time"

	"istio.io/istio/mixer/adapter/appoptics/config"
	"istio.io/istio/mixer/adapter/appoptics/papertrail"
	"istio.io/istio/mixer/template/metric"
)

func TestNewMetricsHandler(t *testing.T) {
	ctx := context.Background()
	logger := &papertrail.LoggerImpl{}
	type testData struct {
		name string
		cfg  *config.Params
	}
	tests := []testData{
		testData{
			name: "All good",
			cfg: &config.Params{
				AppopticsAccessToken: "asdfsdf",
			},
		},
		testData{
			name: "No access token",
			cfg:  &config.Params{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger.Infof("Starting %s - test run. . .\n", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())

			mhi, err := NewMetricsHandler(ctx, &adapterEnvInst{}, test.cfg)
			if err != nil {
				t.Errorf("Unexpected error while running %s test - %v", t.Name(), err)
				return
			}
			defer mhi.Close()

			mh, ok := mhi.(*metricsHandler)
			if !ok || mh == nil {
				t.Errorf("Instance is not of valid type")
			}
		})
	}
}

func TestHandleMetric(t *testing.T) {
	ctx := context.Background()
	logger := &papertrail.LoggerImpl{}

	t.Run("handle metric", func(t *testing.T) {
		logger.Infof("Starting %s - test run. . .\n", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())

		mhi, err := NewMetricsHandler(ctx, &adapterEnvInst{}, &config.Params{})
		if err != nil {
			t.Errorf("Unexpected error while running %s test - %v", t.Name(), err)
			return
		}
		defer mhi.Close()
		err = mhi.HandleMetric(ctx, []*metric.Instance{
			&metric.Instance{
				Name:  "m1",
				Value: 1, // int
				Dimensions: map[string]interface{}{
					"tag1": 1,
				},
			},
			&metric.Instance{
				Name:  "m2",
				Value: 3.4, // float
				Dimensions: map[string]interface{}{
					"tag2": 3.4,
				},
			},
			&metric.Instance{
				Name:  "m3",
				Value: time.Duration(5 * time.Second), // duration
				Dimensions: map[string]interface{}{
					"tag3": "hello",
				},
			},
			&metric.Instance{
				Name:  "m3",
				Value: "abc", // string
			},
		})

		if err != nil {
			t.Errorf("Unexpected error while running %s test - %v", t.Name(), err)
			return
		}
	})
}
