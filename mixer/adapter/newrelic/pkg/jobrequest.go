package pkg

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	policy "istio.io/api/policy/v1beta1"

	"istio.io/istio/mixer/template/metric"
)

// JobRequest presents the struct of payload that contains telemetry data come from mixer
// and insert Insight Event through NewRelic REST API
type JobRequest struct {
	PayLoad []*metric.InstanceMsg
}

var (
	nrapikey         string
	nraccountid      string
	insightsEndpoint string
)

func (j *JobRequest) sendInsightEvents() {
	var b []byte
	var buffer bytes.Buffer
	buffer.WriteString("[")

	for _, inst := range j.PayLoad {
		buffer.WriteString(`{"eventType":"IstioMetrics",`)
		buffer.WriteString(`"metricName":"` + inst.Name + `",`)

		instValue := inst.Value.GetValue()
		switch valueType := instValue.(type) {
		case *policy.Value_StringValue:
			buffer.WriteString(`"metricValue":"` + fmt.Sprintf("%v", valueType.StringValue) + `"`)
		case *policy.Value_Int64Value:
			buffer.WriteString(`"metricValue":` + fmt.Sprintf("%v", valueType.Int64Value))
		case *policy.Value_DoubleValue:
			buffer.WriteString(`"metricValue":` + fmt.Sprintf("%v", valueType.DoubleValue))
		case *policy.Value_BoolValue:
			buffer.WriteString(`"metricValue":"` + strconv.FormatBool(valueType.BoolValue) + `"`)
		default:
			buffer.WriteString(`"metricValue":"` + fmt.Sprintf("%v", instValue) + `"`)
		}

		for key, value := range inst.Dimensions {
			switch dtype := value.GetValue().(type) {
			case *policy.Value_StringValue:
				buffer.WriteString(`,"` + string(key) + `":"` + fmt.Sprintf("%v", dtype.StringValue) + `"`)
			case *policy.Value_Int64Value:
				buffer.WriteString(`,"` + string(key) + `":` + fmt.Sprintf("%v", dtype.Int64Value))
			case *policy.Value_DoubleValue:
				buffer.WriteString(`,"` + string(key) + `":` + fmt.Sprintf("%v", dtype.DoubleValue))
			case *policy.Value_BoolValue:
				buffer.WriteString(`,"` + string(key) + `":"` + strconv.FormatBool(dtype.BoolValue) + `"`)
			default:
				buffer.WriteString(`,"` + string(key) + `":"` + fmt.Sprintf("%v", value.GetValue()) + `"`)
			}
			//buffer.WriteString(`,"` + string(key) + `":"` + fmt.Sprintf("%v", decodeValue(value.GetValue())) + `"`)
		}
		buffer.WriteString("},")
	}

	b = bytes.TrimSuffix(buffer.Bytes(), []byte(","))
	b = append(b, []byte("]")...)
	go sendOutMetrics(b)
	fmt.Printf(buffer.String() + "\n")
}

// SendHttpRequest initialize an HTTP client and call NewRelic REST API
func SendHttpRequest(method string, key string, link string, body io.Reader) (string, error) {
	req, err := http.NewRequest(method, link, body)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Insert-Key", key)
	}

	req.Close = true
	client := &http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 10 * time.Second,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Do(req)

	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	res, _ := ioutil.ReadAll(resp.Body)
	return string(res), nil
}

func sendOutMetrics(metric []byte) {
	feedback, err := SendHttpRequest("POST", nrapikey, insightsEndpoint, bytes.NewReader(metric))
	if err != nil {
		fmt.Println("err: send to backend " + string(err.Error()))
		return
	}
	fmt.Println(string(feedback))
}

func init() {
	nrapikey = os.Getenv("NEW_RELIC_APIKEY")
	nraccountid = os.Getenv("NEW_RELIC_ACCOUNT")
	if nrapikey == "" {
		fmt.Printf("!!! No NEW_RELIC_APIKEY detected !!!\n\n")
		os.Exit(1)
	}

	if nraccountid == "" {
		fmt.Printf("!!! No NEW_RELIC_ACCOUNT detected !!!\n\n")
		os.Exit(1)
	}

	insightsEndpoint = fmt.Sprintf("https://insights-collector.newrelic.com/v1/accounts/%s/events", nraccountid)
}
