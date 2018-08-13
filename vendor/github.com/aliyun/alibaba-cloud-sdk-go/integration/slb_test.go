package integration

import (
	"fmt"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/utils"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
	"github.com/stretchr/testify/assert"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	SlbInstanceStopped = "inactive"
)

// create -> start -> stop -> delete
func TestSlbInstance(t *testing.T) {

	// init client
	config := getConfigFromEnv()
	slbClient, err := slb.NewClientWithAccessKey("cn-hangzhou", config.AccessKeyId, config.AccessKeySecret)
	assertErrorNil(t, err, "Failed to init client")
	fmt.Printf("Init client success\n")

	// create
	instanceId := createSlbInstance(t, slbClient)

	// defer delete
	defer deleteSlbInstance(t, slbClient, instanceId)

	// add backend server
	addBackEndServer(t, slbClient, instanceId)

	// set weight to 80
	setBackEndServer(t, slbClient, instanceId)

	// remove backend server
	removeBackEndServer(t, slbClient, instanceId)

	// stop
	stopSlbInstance(t, slbClient, instanceId)

	// delete all test instance
	deleteAllTestSlbInstance(t, slbClient)
}

func createSlbInstance(t *testing.T, client *slb.Client) (instanceId string) {
	fmt.Print("creating slb instance...")
	request := slb.CreateCreateLoadBalancerRequest()
	request.LoadBalancerName = InstanceNamePrefix + strconv.FormatInt(time.Now().Unix(), 10)
	request.AddressType = "internet"
	request.ClientToken = utils.GetUUIDV4()
	response, err := client.CreateLoadBalancer(request)
	assertErrorNil(t, err, "Failed to create slb instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	instanceId = response.LoadBalancerId
	fmt.Printf("success(%d)! loadBalancerId = %s\n", response.GetHttpStatus(), instanceId)
	return
}

func stopSlbInstance(t *testing.T, client *slb.Client, instanceId string) {
	fmt.Printf("stopping slb instance(%s)...", instanceId)
	request := slb.CreateSetLoadBalancerStatusRequest()
	request.LoadBalancerId = instanceId
	request.LoadBalancerStatus = SlbInstanceStopped
	response, err := client.SetLoadBalancerStatus(request)
	assertErrorNil(t, err, "Failed to stop slb instance "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func deleteSlbInstance(t *testing.T, client *slb.Client, instanceId string) {
	fmt.Printf("deleting slb instance(%s)...", instanceId)
	request := slb.CreateDeleteLoadBalancerRequest()
	request.LoadBalancerId = instanceId
	response, err := client.DeleteLoadBalancer(request)
	if response != nil && response.GetHttpStatus() == http.StatusNotFound {
		fmt.Println("success!")
	} else {
		assertErrorNil(t, err, "Failed to delete slb instance "+instanceId)
		assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
		fmt.Println("success!")
	}
}

func addBackEndServer(t *testing.T, client *slb.Client, instanceId string) {
	fmt.Printf("add backend server for slb(%s)...", instanceId)
	ecsDemoInstanceId := getEcsDemoInstanceId()
	request := slb.CreateSetBackendServersRequest()
	request.BackendServers = fmt.Sprintf("[{\"ServerId\":\"%s\",\"Weight\":\"100\"}]", ecsDemoInstanceId)
	request.LoadBalancerId = instanceId
	response, err := client.SetBackendServers(request)
	assertErrorNil(t, err, "Failed to add backend servers, LoadBalancerId: "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func setBackEndServer(t *testing.T, client *slb.Client, instanceId string) {
	fmt.Printf("set backend server for slb(%s)...", instanceId)
	ecsDemoInstanceId := getEcsDemoInstanceId()
	request := slb.CreateSetBackendServersRequest()
	request.BackendServers = fmt.Sprintf("[{\"ServerId\":\"%s\",\"Weight\":\"80\"}]", ecsDemoInstanceId)
	request.LoadBalancerId = instanceId
	response, err := client.SetBackendServers(request)
	assertErrorNil(t, err, "Failed to set backend servers, LoadBalancerId: "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func removeBackEndServer(t *testing.T, client *slb.Client, instanceId string) {
	fmt.Printf("remove backend server for slb(%s)...", instanceId)
	ecsDemoInstanceId := getEcsDemoInstanceId()
	request := slb.CreateRemoveBackendServersRequest()
	request.BackendServers = fmt.Sprintf("[\"%s\"]", ecsDemoInstanceId)
	request.LoadBalancerId = instanceId
	response, err := client.RemoveBackendServers(request)
	assertErrorNil(t, err, "Failed to remove backend servers, LoadBalancerId: "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func deleteAllTestSlbInstance(t *testing.T, client *slb.Client) {
	fmt.Printf("list all slb instances...")
	request := slb.CreateDescribeLoadBalancersRequest()
	response, err := client.DescribeLoadBalancers(request)
	assertErrorNil(t, err, "Failed to list all slb instances")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)! TotalCount = %s\n", response.GetHttpStatus(), strconv.Itoa(response.TotalCount))

	for _, slbInstanceInfo := range response.LoadBalancers.LoadBalancer {
		if strings.HasPrefix(slbInstanceInfo.LoadBalancerName, InstanceNamePrefix) {
			createTime, err := strconv.ParseInt(slbInstanceInfo.LoadBalancerName[len(InstanceNamePrefix):], 10, 64)
			assertErrorNil(t, err, "Parse instance create time failed: "+slbInstanceInfo.LoadBalancerName)
			if (time.Now().Unix() - createTime) < (60 * 60) {
				fmt.Printf("found undeleted slb instance(%s) but created in 60 minutes, try to delete next time\n", slbInstanceInfo.LoadBalancerName)
			} else {
				fmt.Printf("found undeleted slb instance(%s), status=%s, try to delete it.\n",
					slbInstanceInfo.LoadBalancerId, slbInstanceInfo.LoadBalancerStatus)
				if slbInstanceInfo.LoadBalancerStatus != SlbInstanceStopped {
					// stop
					stopSlbInstance(t, client, slbInstanceInfo.LoadBalancerId)
				}
				// delete
				deleteSlbInstance(t, client, slbInstanceInfo.LoadBalancerId)
			}
		}
	}
}
