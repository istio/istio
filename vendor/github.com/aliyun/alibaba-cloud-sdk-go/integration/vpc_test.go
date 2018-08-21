package integration

import (
	"fmt"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/vpc"
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
	"time"
)

func TestVpcInstance(t *testing.T) {

	// init client
	config := getConfigFromEnv()
	vpcClient, err := vpc.NewClientWithAccessKey("cn-hangzhou", config.AccessKeyId, config.AccessKeySecret)
	assertErrorNil(t, err, "Failed to init client")
	fmt.Printf("Init client success\n")

	// create vpc
	vpcId := createVpcInstance(t, vpcClient)

	time.Sleep(5 * time.Second)

	// create switch
	vswitchId := createVswitchInstance(t, vpcClient, vpcId)

	time.Sleep(5 * time.Second)

	// delete vswitch
	deleteVSwitchInstance(t, vpcClient, vswitchId)

	// delete vpc
	deleteVpcInstance(t, vpcClient, vpcId)
}

func createVpcInstance(t *testing.T, client *vpc.Client) (vpcId string) {
	fmt.Print("creating vpc instance...")
	request := vpc.CreateCreateVpcRequest()
	request.VpcName = InstanceNamePrefix + strconv.FormatInt(time.Now().Unix(), 10)
	request.CidrBlock = "192.168.0.0/16"
	response, err := client.CreateVpc(request)
	assertErrorNil(t, err, "Failed to create vpc instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	vpcId = response.VpcId
	fmt.Printf("success(%d)! vpcId = %s\n", response.GetHttpStatus(), vpcId)
	return
}

func createVswitchInstance(t *testing.T, client *vpc.Client, vpcId string) (vswitchId string) {
	fmt.Print("creating vswitch instance...")
	request := vpc.CreateCreateVSwitchRequest()
	request.VSwitchName = InstanceNamePrefix + strconv.FormatInt(time.Now().Unix(), 10)
	request.VpcId = vpcId
	request.ZoneId = "cn-hangzhou-b"
	request.CidrBlock = "192.168.3.0/24"
	response, err := client.CreateVSwitch(request)
	assertErrorNil(t, err, "Failed to create vswitch instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	vswitchId = response.VSwitchId
	fmt.Printf("success(%d)! VSwitchId = %s\n", response.GetHttpStatus(), vpcId)
	return
}

func deleteVSwitchInstance(t *testing.T, client *vpc.Client, vswitchId string) {
	fmt.Printf("deleting vswitch instance(%s)...", vswitchId)
	request := vpc.CreateDeleteVSwitchRequest()
	request.VSwitchId = vswitchId
	response, err := client.DeleteVSwitch(request)
	assertErrorNil(t, err, "Failed to delete vswitch instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)!\n", response.GetHttpStatus())
	return
}

func deleteVpcInstance(t *testing.T, client *vpc.Client, vpcId string) {
	fmt.Printf("deleting vpc instance(%s)...", vpcId)
	request := vpc.CreateDeleteVpcRequest()
	request.VpcId = vpcId
	response, err := client.DeleteVpc(request)
	assertErrorNil(t, err, "Failed to delete vpc instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)!\n", response.GetHttpStatus())
	return
}
