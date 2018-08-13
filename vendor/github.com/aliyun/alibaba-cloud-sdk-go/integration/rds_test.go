package integration

import (
	"fmt"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/stretchr/testify/assert"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	RdsInstanceDefaultTimeout = 240
	RdsDefaultWaitForInterval = 60

	RdsInstanceStatusRunning = "Running"
	RdsInstanceStatusStopped = "Stopped"
	RdsInstanceStatusDeleted = "Deleted"
)

func TestRdsInstance(t *testing.T) {

	// init client
	testConfig := getConfigFromEnv()
	credential := &credentials.BaseCredential{
		AccessKeyId:     testConfig.AccessKeyId,
		AccessKeySecret: testConfig.AccessKeySecret,
	}
	clientConfig := sdk.NewConfig().WithTimeout(30 * time.Second)
	rdsClient, err := rds.NewClientWithOptions("cn-hangzhou", clientConfig, credential)
	assertErrorNil(t, err, "Failed to init client")
	fmt.Printf("Init client success\n")

	dbInstanceId := createDBInstance(t, rdsClient)

	waitForRdsInstance(t, rdsClient, dbInstanceId, RdsInstanceStatusRunning, 2400)

	//createAccount(t, rdsClient, dbInstanceId)

	//nodeId := getHAConfig(t, rdsClient, dbInstanceId)
	//
	//changeNodeToMaster(t, rdsClient, dbInstanceId, nodeId)
	//
	//createDataBase(t, rdsClient, dbInstanceId)

	deleteDBInstance(t, rdsClient, dbInstanceId)

	deleteAllTestRdsInstance(t, rdsClient)
}

func createDBInstance(t *testing.T, client *rds.Client) (rdsInstanceId string) {
	fmt.Print("creating rds mysql instance...")
	request := rds.CreateCreateDBInstanceRequest()
	request.Engine = "MySQL"
	request.EngineVersion = "5.7"
	request.DBInstanceClass = "mysql.n1.micro.1"
	request.DBInstanceStorage = "20"
	request.DBInstanceNetType = "Intranet"
	request.SecurityIPList = "0.0.0.0/0"
	request.PayType = "Postpaid"
	request.DBInstanceDescription = InstanceNamePrefix + strconv.FormatInt(time.Now().Unix(), 10)
	response, err := client.CreateDBInstance(request)
	assertErrorNil(t, err, "Failed to create rds mysql instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	rdsInstanceId = response.DBInstanceId
	fmt.Printf("success(%d)! dbInstanceId = %s\n", response.GetHttpStatus(), rdsInstanceId)
	return
}

func getHAConfig(t *testing.T, client *rds.Client, instanceId string) (nodeId string) {
	fmt.Print("get dbInstance HA nodeId...")
	request := rds.CreateDescribeDBInstanceHAConfigRequest()
	request.DBInstanceId = instanceId
	response, err := client.DescribeDBInstanceHAConfig(request)
	assertErrorNil(t, err, "Failed to get dbInstance HA nodeId")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	nodeId = response.HostInstanceInfos.NodeInfo[0].NodeId
	fmt.Printf("success(%d)! nodeId = %s\n", response.GetHttpStatus(), nodeId)
	return
}

func changeNodeToMaster(t *testing.T, client *rds.Client, instanceId, nodeId string) {
	fmt.Print("trying to change current instance to master...")
	request := rds.CreateSwitchDBInstanceHARequest()
	request.DBInstanceId = instanceId
	request.NodeId = nodeId
	response, err := client.SwitchDBInstanceHA(request)
	assertErrorNil(t, err, "Failed to change current instance to master")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)!\n", response.GetHttpStatus())
	return
}

func createAccount(t *testing.T, client *rds.Client, instanceId string) {
	fmt.Print("creating mysql account...")
	request := rds.CreateCreateAccountRequest()
	request.DBInstanceId = instanceId
	request.AccountName = "sdktest"
	request.AccountPassword = strconv.FormatInt(time.Now().Unix(), 10)
	response, err := client.CreateAccount(request)
	assertErrorNil(t, err, "Failed to create mysql account")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)!\n", response.GetHttpStatus())
	return
}

func createDataBase(t *testing.T, client *rds.Client, instanceId string) {
	fmt.Print("creating mysql database...")
	request := rds.CreateCreateDatabaseRequest()
	request.DBInstanceId = instanceId
	request.DBName = "sdktest"
	request.CharacterSetName = "utf8"
	response, err := client.CreateDatabase(request)
	assertErrorNil(t, err, "Failed to create mysql database")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)!\n", response.GetHttpStatus())
	return
}

func deleteDBInstance(t *testing.T, client *rds.Client, instanceId string) {
	fmt.Print("deleting rds instance...")
	request := rds.CreateDeleteDBInstanceRequest()
	request.DBInstanceId = instanceId
	response, err := client.DeleteDBInstance(request)
	assertErrorNil(t, err, "Failed to delete rds instance")
	assert.Equal(t, 200, response.GetHttpStatus(), response.GetHttpContentString())
	fmt.Printf("success(%d)!\n", response.GetHttpStatus())
	return
}

func waitForRdsInstance(t *testing.T, client *rds.Client, instanceId string, targetStatus string, timeout int) {
	if timeout <= 0 {
		timeout = RdsInstanceDefaultTimeout
	}
	for {
		request := rds.CreateDescribeDBInstanceAttributeRequest()
		request.DBInstanceId = instanceId
		response, err := client.DescribeDBInstanceAttribute(request)

		currentStatus := getSingleInstanceStatusFromDescribeDBInstanceAttributeResponse(response)
		if targetStatus == RdsInstanceStatusDeleted {
			if currentStatus == targetStatus {
				fmt.Printf("delete rds instance(%s) success\n", instanceId)
				break
			} else {
				assertErrorNil(t, err, "Failed to describe rds instance \n")
			}
		} else {
			assertErrorNil(t, err, "Failed to describe rds instance \n")
			if currentStatus == targetStatus {
				fmt.Printf("rds instance(%s) status changed to %s, wait a moment\n", instanceId, targetStatus)
				time.Sleep(RdsDefaultWaitForInterval * time.Second)
				break
			} else {
				fmt.Printf("rds instance(%s) status is %s, wait for changing to %s\n", instanceId, currentStatus, targetStatus)
			}
		}

		timeout = timeout - RdsDefaultWaitForInterval
		if timeout <= 0 {
			t.Errorf(fmt.Sprintf("wait for ecs instance(%s) status to %s timeout(%d)\n", instanceId, targetStatus, timeout))
		}
		time.Sleep(RdsDefaultWaitForInterval * time.Second)
	}
}

func deleteAllTestRdsInstance(t *testing.T, client *rds.Client) {
	// list all instances
	fmt.Print("trying to list all rds test instances...")
	listRequest := rds.CreateDescribeDBInstancesRequest()
	listResponse, err := client.DescribeDBInstances(listRequest)
	assertErrorNil(t, err, "Failed to list all rds instances")
	assert.Equal(t, 200, listResponse.GetHttpStatus(), listResponse.GetHttpContentString())

	fmt.Printf("found %s instances\n", strconv.Itoa(listResponse.TotalRecordCount))
	for _, instance := range listResponse.Items.DBInstance {
		if strings.HasPrefix(instance.DBInstanceDescription, InstanceNamePrefix) {
			createTime, err := strconv.ParseInt(instance.DBInstanceDescription[len(InstanceNamePrefix):], 10, 64)
			assertErrorNil(t, err, "Parse instance create time failed: "+instance.DBInstanceDescription)
			if (time.Now().Unix() - createTime) < (60 * 60) {
				fmt.Printf("found undeleted rds instance(%s) but created in 60 minutes, try to delete next time\n", instance.DBInstanceDescription)
			} else {
				fmt.Printf("found rds test instance(%s), trying to delte it\n", instance.DBInstanceId)
				deleteDBInstance(t, client, instance.DBInstanceId)
			}
		}
	}
}

func getSingleInstanceStatusFromDescribeDBInstanceAttributeResponse(response *rds.DescribeDBInstanceAttributeResponse) string {
	if response.GetHttpStatus() == 404 || len(response.Items.DBInstanceAttribute) == 0 {
		return RdsInstanceStatusDeleted
	}
	return response.Items.DBInstanceAttribute[0].DBInstanceStatus
}
