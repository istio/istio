/*
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package sdk

import (
	"crypto/tls"
	"encoding/json"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"testing"
)

var client, clientKeyPair, clientEcs, clientRoleArn, clientSts *Client

type TestConfig struct {
	AccessKeyId     string
	AccessKeySecret string
	PublicKeyId     string
	PrivateKey      string
	RoleArn         string
	StsToken        string
	StsAk           string
	StsSecret       string
	ChildAK         string
	ChildSecret     string
}

type MockResponse struct {
	Headers     map[string]string
	Body        string
	Params      map[string]string
	RemoteAddr  string
	RemoteHost  string
	QueryString string
	RequestURL  string
}

func TestMain(m *testing.M) {
	testSetup()
	result := m.Run()
	testTearDown()
	os.Exit(result)
}

func getConfigFromFile() *TestConfig {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	configFilePath := usr.HomeDir + "/aliyun-sdk.json"
	data, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		panic(err)
	}
	var config TestConfig
	json.Unmarshal(data, &config)
	return &config
}

func getConfigFromEnv() *TestConfig {
	config := &TestConfig{
		AccessKeyId:     os.Getenv("ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("ACCESS_KEY_SECRET"),
		PublicKeyId:     os.Getenv("PUBLIC_KEY_ID"),
		PrivateKey:      os.Getenv("PRIVATE_KEY"),
		RoleArn:         os.Getenv("ROLE_ARN"),
		ChildAK:         os.Getenv("CHILD_AK"),
		ChildSecret:     os.Getenv("CHILD_SECRET"),
		StsToken:        os.Getenv("STS_TOKEN"),
		StsAk:           os.Getenv("STS_AK"),
		StsSecret:       os.Getenv("STS_SECRET"),
	}
	if config.AccessKeyId == "" || os.Getenv("ENV_TYPE") != "CI" {
		return nil
	} else {
		return config
	}
}

func testSetup() {
	testConfig := getConfigFromEnv()
	if testConfig == nil {
		testConfig = getConfigFromFile()
	}

	var err error

	clientConfig := NewConfig().
		WithEnableAsync(true).
		WithGoRoutinePoolSize(5).
		WithMaxTaskQueueSize(1000).
		WithHttpTransport(&http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		})
		//}).
		//WithMaxRetryTime(15).
		//WithTimeout(10)

	credential := &credentials.BaseCredential{
		AccessKeyId:     testConfig.AccessKeyId,
		AccessKeySecret: testConfig.AccessKeySecret,
	}
	client, err = NewClientWithOptions("cn-hangzhou", clientConfig, credential)
	if err != nil {
		panic(err)
	}

	rsaKeypairCredential := credentials.NewRsaKeyPairCredential(testConfig.PrivateKey, testConfig.PublicKeyId, 3600)
	clientKeyPair, err = NewClientWithOptions("cn-hangzhou", clientConfig, rsaKeypairCredential)
	if err != nil {
		panic(err)
	}

	roleNameOnEcsCredential := credentials.NewStsRoleNameOnEcsCredential("conan")
	clientEcs, err = NewClientWithOptions("cn-hangzhou", clientConfig, roleNameOnEcsCredential)
	if err != nil {
		panic(err)
	}

	stsRoleArnCredential := credentials.NewStsRoleArnCredential(testConfig.ChildAK, testConfig.ChildSecret, testConfig.RoleArn, "clientTest", 3600)
	clientRoleArn, err = NewClientWithOptions("cn-hangzhou", clientConfig, stsRoleArnCredential)
	if err != nil {
		panic(err)
	}

	stsCredential := credentials.NewStsTokenCredential(testConfig.StsAk, testConfig.StsSecret, testConfig.StsToken)
	clientSts, err = NewClientWithOptions("cn-hangzhou", clientConfig, stsCredential)
	if err != nil {
		panic(err)
	}
}

func testTearDown() {

}

func TestNewClientWithAccessKey(t *testing.T) {
	assert.NotNil(t, client, "NewClientWithAccessKey failed")
}

func TestRoaGet(t *testing.T) {
	request := getFtTestRoaRequest()

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "HeaderParamValue", responseBean.Headers["Header-Param"])
}

func TestRoaPostForm(t *testing.T) {
	request := getFtTestRoaRequest()
	request.Method = requests.POST
	request.FormParams["BodyParam"] = "BodyParamValue"

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "HeaderParamValue", responseBean.Headers["Header-Param"])
	assert.Equal(t, "BodyParamValue", responseBean.Params["BodyParam"])
}

func TestRoaPostStream(t *testing.T) {
	request := getFtTestRoaRequest()
	request.Method = requests.POST
	request.Content = []byte("TestContent")

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "HeaderParamValue", responseBean.Headers["Header-Param"])
	assert.Equal(t, "TestContent", responseBean.Body)
}

func TestRoaPostJson(t *testing.T) {
	request := getFtTestRoaRequest()
	request.Method = requests.POST
	dataMap := map[string]string{"key": "value"}
	data, err := json.Marshal(dataMap)
	assert.Nil(t, err)
	request.Content = data
	request.SetContentType(requests.Json)

	response := &responses.BaseResponse{}
	err = client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "HeaderParamValue", responseBean.Headers["Header-Param"])
	assert.Equal(t, requests.Json, responseBean.Headers["Content-Type"])
	assert.Equal(t, string(data), responseBean.Body)
}

func TestRpcGet(t *testing.T) {
	request := getFtTestRpcRequest()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

func TestRpcGetForHttps(t *testing.T) {
	request := getFtTestRpcRequest()
	request.Method = requests.GET
	request.Scheme = requests.HTTPS

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

func TestRoaGetForHttps(t *testing.T) {
	request := getFtTestRoaRequest()
	request.Scheme = requests.HTTPS

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "HeaderParamValue", responseBean.Headers["Header-Param"])
}

func TestRpcPost(t *testing.T) {
	request := getFtTestRpcRequest()
	request.FormParams["BodyParam"] = "BodyParamValue"

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "BodyParamValue", responseBean.Params["BodyParam"])
}

func getFtTestRoaRequest() (request *requests.RoaRequest) {
	request = &requests.RoaRequest{}
	request.InitWithApiInfo("Ft", "2016-01-02", "TestRoaApi", "/web/cloudapi", "", "")
	request.Domain = "ft.aliyuncs.com"

	request.Headers["Header-Param"] = "HeaderParamValue"
	request.QueryParams["QueryParam"] = "QueryParamValue"

	return
}

func getFtTestRpcRequest() (request *requests.RpcRequest) {
	request = &requests.RpcRequest{}
	request.InitWithApiInfo("Ft", "2016-01-01", "TestRpcApi", "", "")
	request.Domain = "ft.aliyuncs.com"
	request.QueryParams["QueryParam"] = "QueryParamValue"
	return
}

func getFtTestRpcRequestForEndpointLocation() (request *requests.RpcRequest) {
	request = &requests.RpcRequest{}
	request.InitWithApiInfo("Ft", "2016-01-01", "TestRpcApi", "ft", "openAPI")
	request.RegionId = "ft-cn-hangzhou"
	request.QueryParams["QueryParam"] = "QueryParamValue"
	request.Domain = "ft.aliyuncs.com"
	return
}

func getFtTestRpcRequestForEndpointXml() (request *requests.RpcRequest) {
	request = &requests.RpcRequest{}
	request.InitWithApiInfo("Ft", "2016-01-01", "TestRpcApi", "", "")
	request.RegionId = "cn-hangzhou"
	request.QueryParams["QueryParam"] = "QueryParamValue"
	request.Domain = "ft.aliyuncs.com"
	return
}

func TestCommonRpcRequest(t *testing.T) {
	rpcRequest := requests.NewCommonRequest()
	rpcRequest.Product = "Ft"
	rpcRequest.Version = "2016-01-01"
	rpcRequest.Domain = "ft.aliyuncs.com"
	rpcRequest.ApiName = "TestRpcApi"
	rpcRequest.Method = "POST"

	rpcRequest.QueryParams["QueryParam"] = "QueryParamValue"
	rpcRequest.FormParams["BodyParam"] = "BodyParamValue"

	response, err := client.ProcessCommonRequest(rpcRequest)

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "BodyParamValue", responseBean.Params["BodyParam"])
}

func TestCommonRoaRequest(t *testing.T) {
	roaRequest := requests.NewCommonRequest()
	roaRequest.Product = "Ft"
	roaRequest.Version = "2016-01-02"
	roaRequest.PathPattern = "/web/cloudapi"
	roaRequest.Domain = "ft.aliyuncs.com"
	roaRequest.Method = "POST"

	roaRequest.QueryParams["QueryParam"] = "QueryParamValue"
	roaRequest.FormParams["BodyParam"] = "BodyParamValue"

	response, err := client.ProcessCommonRequest(roaRequest)

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
	assert.Equal(t, "BodyParamValue", responseBean.Params["BodyParam"])
}

func TestRpcGetForEndpointXml(t *testing.T) {
	request := getFtTestRpcRequestForEndpointXml()
	request.Method = requests.GET
	request.RegionId = "cn-shanghai"

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

func TestRpcGetForLocation(t *testing.T) {
	request := getFtTestRpcRequestForEndpointLocation()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

func TestRpcGetForLocationCache(t *testing.T) {
	request := getFtTestRpcRequestForEndpointLocation()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := client.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])

	request2 := getFtTestRpcRequestForEndpointLocation()
	request2.Method = requests.GET
	err = client.DoAction(request2, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

func TestRpcGetForKeyPair(t *testing.T) {
	request := getFtTestRpcRequest()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := clientKeyPair.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

/*func TestRpcGetForEcs(t *testing.T) {
	//测试接口，想测试的时候，要替换掉singer_ecs_instance中对应的变量，并且还要提供一个mock服务
	//requestUrl := "http://localhost:3500/latest/meta-data/ram/security-credentials/roleNameTest.json"
	request := getFtTestRpcRequest()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := clientEcs.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])

	err = clientEcs.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}*/

func TestRpcGetForRoleArn(t *testing.T) {
	request := getFtTestRpcRequest()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := clientRoleArn.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])

	err = clientRoleArn.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

func TestRoaGetForRoleArn(t *testing.T) {
	request := getFtTestRoaRequest()
	request.Method = requests.GET

	response := &responses.BaseResponse{}
	err := clientRoleArn.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	var responseBean MockResponse
	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])

	err = clientRoleArn.DoAction(request, response)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())

	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)

	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
}

//测试Sts的时候要先获取一套stsToken和ak，由于有时效性，所以先把代码注释掉，测试的时候先获取stsToken完成后再调用
//func TestRpcGetForSts(t *testing.T) {
//	request := getFtTestRpcRequest()
//	request.Method = requests.GET
//
//	response := &responses.BaseResponse{}
//	err := clientSts.DoAction(request, response)
//	assert.Nil(t, err)
//	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
//	assert.NotNil(t, response.GetHttpContentString())
//
//	var responseBean MockResponse
//	json.Unmarshal([]byte(response.GetHttpContentString()), &responseBean)
//
//	assert.Equal(t, "QueryParamValue", responseBean.Params["QueryParam"])
//}

func TestCommonRoaRequestForAcceptXML(t *testing.T) {
	roaRequest := requests.NewCommonRequest()
	roaRequest.Product = "Acs"
	roaRequest.Version = "2015-01-01"
	roaRequest.ApiName = "GetGlobal"
	roaRequest.PathPattern = "/"
	roaRequest.Domain = "acs.aliyuncs.com"
	roaRequest.AcceptFormat = "XML"

	response, err := client.ProcessCommonRequest(roaRequest)

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, response.GetHttpStatus(), response.GetHttpContentString())
	assert.NotNil(t, response.GetHttpContentString())
}
