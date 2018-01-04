package appoptics

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"istio.io/istio/mixer/adapter/appoptics/papertrail"
)

func TestCreate(t *testing.T) {
	t.Run("All good", func(t *testing.T) {
		logger := &papertrail.LoggerImpl{}
		logger.Infof("Starting %s - test run. . .\n", t.Name())
		defer logger.Infof("Finishing %s - test run. . .\n", t.Name())

		var count int64

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&count, 1)
		}))
		defer ts.Close()

		c := NewClient("some string", logger)
		c.baseURL, _ = url.Parse(ts.URL)

		resp, err := c.MeasurementsService().Create([]*Measurement{
			&Measurement{}, &Measurement{}, &Measurement{},
		})
		if err != nil {
			t.Errorf("There was an error: %v", err)
		}
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Error("Unexpected response")
		}

		if count != 1 {
			t.Errorf("Received requests dont match expected.")
		}
	})
}
