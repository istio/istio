package integration

import (
	"fmt"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/stretchr/testify/assert"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	EcsInstanceDefaultTimeout = 120
	EcsDefaultWaitForInterval = 20

	EcsInstanceStatusRunning = "Running"
	EcsInstanceStatusStopped = "Stopped"
	EcsInstanceStatusDeleted = "Deleted"
)

// create -> start -> stop -> delete
func TestEcsInstance(t *testing.T) {

	// init client
	config := getConfigFromEnv()
	ecsClient, err := ecs.NewClientWithAccessKey("cn-hangzhou", config.AccessKeyId, config.AccessKeySecret)
	assertErrorNil(t, err, "Failed to init client")
	fmt.Printf("Init client success\n")

	// get demo instance attributes
	param := getDemoEcsInstanceAttributes(t, ecsClient)

	// create
	instanceId := createEcsInstance(t, ecsClient, param)

	// defer wait for deleted
	defer waitForEcsInstance(t, ecsClient, instanceId, EcsInstanceStatusDeleted, 120)

	// defer delete
	defer deleteEcsInstance(t, ecsClient, instanceId)

	// wait
	waitForEcsInstance(t, ecsClient, instanceId, EcsInstanceStatusStopped, 60)

	// start
	startEcsInstance(t, ecsClient, instanceId)

	// wait
	waitForEcsInstance(t, ecsClient, instanceId, EcsInstanceStatusRunning, 240)

	// stop
	stopEcsInstance(t, ecsClient, instanceId)

	// wait
	waitForEcsInstance(t, ecsClient, instanceId, EcsInstanceStatusStopped, 600)

	//delete all test instance
	deleteAllTestEcsInstance(t, ecsClient)
}

func getDemoEcsInstanceAttributes(t *testing.T, client *ecs.Client) *ecs.DescribeInstanceAttributeResponse {
	fmt.Print("trying to get demo ecs instance...")
	request := ecs.CreateDescribeInstanceAttributeRequest()
	request.InstanceId = getEcsDemoInstanceId()
	response, err := client.DescribeInstanceAttribute(request)
	assertErrorNil(t, err, "Failed to get demo ecs instance attributes")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
	return response
}

func createEcsInstance(t *testing.T, client *ecs.Client, param *ecs.DescribeInstanceAttributeResponse) (instanceId string) {
	fmt.Print("creating ecs instance...")
	request := ecs.CreateCreateInstanceRequest()
	request.ImageId = param.ImageId
	request.InstanceName = InstanceNamePrefix + strconv.FormatInt(time.Now().Unix(), 10)
	request.SecurityGroupId = param.SecurityGroupIds.SecurityGroupId[0]
	request.InstanceType = "ecs.t1.small"
	response, err := client.CreateInstance(request)
	assertErrorNil(t, err, "Failed to create ecs instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	instanceId = response.InstanceId
	fmt.Printf("success(%d)! instanceId = %s\n", response.GetHttpStatus(), instanceId)
	return
}

func startEcsInstance(t *testing.T, client *ecs.Client, instanceId string) {
	fmt.Printf("starting ecs instance(%s)...", instanceId)
	request := ecs.CreateStartInstanceRequest()
	request.InstanceId = instanceId
	response, err := client.StartInstance(request)
	assertErrorNil(t, err, "Failed to start ecs instance "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func stopEcsInstance(t *testing.T, client *ecs.Client, instanceId string) {
	fmt.Printf("stopping ecs instance(%s)...", instanceId)
	request := ecs.CreateStopInstanceRequest()
	request.InstanceId = instanceId
	response, err := client.StopInstance(request)
	assertErrorNil(t, err, "Failed to stop ecs instance "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func deleteEcsInstance(t *testing.T, client *ecs.Client, instanceId string) {
	fmt.Printf("deleting ecs instance(%s)...", instanceId)
	request := ecs.CreateDeleteInstanceRequest()
	request.InstanceId = instanceId
	response, err := client.DeleteInstance(request)
	assertErrorNil(t, err, "Failed to delete ecs instance "+instanceId)
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Println("success!")
}

func deleteAllTestEcsInstance(t *testing.T, client *ecs.Client) {
	fmt.Print("list all ecs instances...")
	request := ecs.CreateDescribeInstancesRequest()
	request.PageSize = requests.NewInteger(30)
	request.PageNumber = requests.NewInteger(1)
	response, err := client.DescribeInstances(request)
	assertErrorNil(t, err, "Failed to list all ecs instances ")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success! TotalCount = %s\n", strconv.Itoa(response.TotalCount))

	for _, instanceInfo := range response.Instances.Instance {
		if strings.HasPrefix(instanceInfo.InstanceName, InstanceNamePrefix) {
			createTime, err := strconv.ParseInt(instanceInfo.InstanceName[26:len(instanceInfo.InstanceName)], 10, 64)
			assertErrorNil(t, err, "Parse instance create time failed: "+instanceInfo.InstanceName)
			if (time.Now().Unix() - createTime) < (60 * 60) {
				fmt.Printf("found undeleted ecs instance(%s) but created in 60 minutes, try to delete next time\n", instanceInfo.InstanceName)
			} else {
				fmt.Printf("found undeleted ecs instance(%s), status=%s, try to delete it.\n",
					instanceInfo.Status, instanceInfo.InstanceId)
				if instanceInfo.Status == EcsInstanceStatusRunning {
					// stop
					stopEcsInstance(t, client, instanceInfo.InstanceId)
				} else if instanceInfo.Status == EcsInstanceStatusStopped {
					// delete
					deleteEcsInstance(t, client, instanceInfo.InstanceId)
					// wait
					waitForEcsInstance(t, client, instanceInfo.InstanceId, EcsInstanceStatusDeleted, 600)
				}
			}
		}
	}
}

func waitForEcsInstance(t *testing.T, client *ecs.Client, instanceId string, targetStatus string, timeout int) {
	if timeout <= 0 {
		timeout = EcsInstanceDefaultTimeout
	}
	for {
		request := ecs.CreateDescribeInstanceAttributeRequest()
		request.InstanceId = instanceId
		response, err := client.DescribeInstanceAttribute(request)

		if targetStatus == EcsInstanceStatusDeleted {
			if response.GetHttpStatus() == 404 || response.Status == EcsInstanceStatusDeleted {
				fmt.Printf("delete ecs instance(%s) success\n", instanceId)
				break
			} else {
				assertErrorNil(t, err, "Failed to describe ecs instance \n")
			}
		} else {
			assertErrorNil(t, err, "Failed to describe ecs instance \n")
			if response.Status == targetStatus {
				fmt.Printf("ecs instance(%s) status changed to %s, wait a moment\n", instanceId, targetStatus)
				time.Sleep(EcsDefaultWaitForInterval * time.Second)
				break
			} else {
				fmt.Printf("ecs instance(%s) status is %s, wait for changing to %s\n", instanceId, response.Status, targetStatus)
			}
		}

		timeout = timeout - EcsDefaultWaitForInterval
		if timeout <= 0 {
			t.Errorf(fmt.Sprintf("wait for ecs instance(%s) status to %s timeout(%d)\n", instanceId, targetStatus, timeout))
		}
		time.Sleep(EcsDefaultWaitForInterval * time.Second)
	}
}
