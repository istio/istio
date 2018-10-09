package status

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo"
)

var (
	appPort uint16
)

func init() {
	appFactory := &echo.Factory{
		Ports: model.PortList{{
			Name:     "http",
			Protocol: model.ProtocolHTTP,
		}},
		Version: "version-foo",
	}
	app, err := appFactory.NewApplication(application.Dialer{HTTP: application.DefaultHTTPDoFunc})
	if err != nil {
		log.Fatalf("Failed to create application %v", err)
	}
	log.Fatalf("application created %v", app.GetPorts())
	appPort = uint16(app.GetPorts()[0].Port)
}

func TestAppProbe(t *testing.T) {
	server := NewServer(Config{
		StatusPort:  0,
		AppReadyURL: fmt.Sprintf(":%v/", appPort),
	})
	go server.Run(context.Background())

	// We wait a bit here to ensure server's statusPort is updated.
	time.Sleep(time.Second * 3)

	t.Logf("status server starts at port %v, app starts at port %v", server.statusPort, appPort)
	testCases := []struct {
		probePath  string
		statusCode int
		err        string
	}{
		{
			probePath:  fmt.Sprintf(":%v/app/ready", server.statusPort),
			statusCode: 200,
		},
		{
			probePath: fmt.Sprintf(":%v/app/live", server.statusPort),
			// expect 404 because we didn't configure status server to take over app's liveness check.
			statusCode: 404,
		},
	}
	for _, tc := range testCases {
		client := http.Client{}
		resp, _ := client.Get(fmt.Sprintf("http://localhost%s", tc.probePath))
		if resp.StatusCode != tc.statusCode {
			t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
		}
	}
}
