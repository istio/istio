# 阿里云Go SDK
[![Build Status](https://travis-ci.org/aliyun/alibaba-cloud-sdk-go.svg?branch=master)](https://travis-ci.org/aliyun/alibaba-cloud-sdk-go) 
[![Go Report Card](https://goreportcard.com/badge/github.com/aliyun/alibaba-cloud-sdk-go)](https://goreportcard.com/report/github.com/aliyun/alibaba-cloud-sdk-go)

欢迎使用阿里云开发者工具套件（SDK）。阿里云Go SDK让您不用复杂编程即可访问云服务器、云监控等多个阿里云服务。这里向您介绍如何获取阿里云Go SDK并开始调用。

如果您在使用SDK的过程中遇上任何问题，欢迎加入 **钉钉群: 11771185(阿里云官方SDK客户服务群)** 咨询

## 环境准备
1. 要使用阿里云Go SDK，您需要一个云账号以及一对`Access Key ID`和`Access Key Secret`。 请在阿里云控制台中的[AccessKey管理页面](https://usercenter.console.aliyun.com/?spm=5176.doc52740.2.3.QKZk8w#/manage/ak)上创建和查看您的Access Key，或者联系您的系统管理员
2. 要使用阿里云SDK访问某个产品的API，您需要事先在[阿里云控制台](https://home.console.aliyun.com/?spm=5176.doc52740.2.4.QKZk8w)中开通这个产品。

## SDK获取和安装

使用`go get`下载安装SDK

```
go get -u github.com/aliyun/alibaba-cloud-sdk-go/sdk
```

如果您使用了glide管理依赖，您也可以使用glide来安装阿里云GO SDK

```
glide get github.com/aliyun/alibaba-cloud-sdk-go
```

另外，阿里云Go SDK也会发布在 https://develop.aliyun.com/tools/sdk#/go 这个地址。

## 开始调用
以下这个代码示例向您展示了调用阿里云GO SDK的3个主要步骤：

1. 创建Client实例
2. 创建API请求并设置参数
3. 发起请求并处理异常

```go
package main

import (
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"fmt"
)

func main() { 
    // 创建ecsClient实例
    ecsClient, err := ecs.NewClientWithAccessKey(
        "<your-region-id>", 			// 您的可用区ID
        "<your-access-key-id>", 		// 您的Access Key ID
        "<your-access-key-secret>")		// 您的Access Key Secret
    if err != nil {
    	// 异常处理
    	panic(err)
    }
    // 创建API请求并设置参数
    request := ecs.CreateDescribeInstancesRequest()
    request.PageSize = "10"
    // 发起请求并处理异常
    response, err := ecsClient.DescribeInstances(request)
    if err != nil {
    	// 异常处理
    	panic(err)
    }
    fmt.Println(response)
}
```

在创建Client实例时，您需要填写3个参数：`Region ID`、`Access Key ID`和`Access Key Secret`。`Access Key ID`和`Access Key Secret`可以从控制台获得；而`Region ID`可以从[地域列表](https://help.aliyun.com/document_detail/40654.html?spm=5176.doc52740.2.8.FogWrd)中获得


## Keepalive
阿里云Go SDK底层使用Go语言原生的`net/http`收发请求，因此配置方式与`net/http`相同，您可以通过config直接将配置传递给底层的httpClient
```go
httpTransport := http.Transport{
	// set http client options
}
config := sdk.NewConfig()
            .WithHttpTransport(httpTransport)
            .WithTimeout(timeout)
ecsClient, err := ecs.NewClientWithOptions(config)

```

## 并发请求

* 因Go语言的并发特性，我们建议您在应用层面控制SDK的并发请求。
* 为了方便您的使用，我们也提供了可直接使用的并发调用方式，相关的并发控制由SDK内部实现。

##### 开启SDK Client的并发功能
```go
// 最大并发数
poolSize := 2
// 可缓存的最大请求数
maxTaskQueueSize := 5

// 在创建时开启异步功能
config := sdk.NewConfig()
            .WithEnableAsync(true)
            .WithGoRoutinePoolSize(poolSize)            // 可选，默认5
            .WithMaxTaskQueueSize(maxTaskQueueSize)     // 可选，默认1000
ecsClient, err := ecs.NewClientWithOptions(config)            

// 也可以在client初始化后再开启
client.EnableAsync(poolSize, maxTaskQueueSize)
```

##### 发起异步调用
阿里云Go SDK支持两种方式的异步调用：

1. 使用channel作为返回值
    ```go
    responseChannel, errChannel := client.FooWithChan(request)
    
    // this will block
    response := <-responseChannel
    err = <-errChannel
    ```

2. 使用callback控制回调
    
    ```go
    blocker := client.FooWithCallback(request, func(response *FooResponse, err error) {
    		// handle the response and err
    	})
 	
    // blocker 为(chan int)，用于控制同步，返回1为成功，0为失败
    // 在<-blocker返回失败时，err依然会被传入的callback处理
    result := <-blocker
    ```
    
## 泛化调用接口(CommonApi)

##### 什么是CommonAPI
CommonAPI是阿里云SDK推出的，泛用型的API调用方式。CommonAPI具有以下几个特点：
1. 轻量：只需Core包即可发起调用，无需下载安装各产品线SDK。
2. 简便：无需更新SDK即可调用最新发布的API。
3. 快速迭代

##### 开始使用

CommonAPI，需要配合相应的API文档使用，以查询API的相关信息。

您可以在 [文档中心](https://help.aliyun.com/?spm=5176.8142029.388261.173.23896dfaav2hEF) 查询到所有产品的API文档。

发起一次CommonAPI请求，需要您查询到以下几个参数：
* 域名(domain)：即该产品的通用访问域名，一版可以在"调用方式"页查看到
* API版本(version)：即该API的版本号，以’YYYY-MM-DD’的形式表现，一般可以在"公共参数"页面查到
* 接口名称(apiName)：即该API的名称

我们以Ecs产品的[DescribeInstanceStatus API](https://help.aliyun.com/document_detail/25505.html?spm=5176.doc25506.6.820.VbHnW6)为例
```go
package main

import (
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"fmt"
)

func main() {

	client, err := sdk.NewClientWithAccessKey("cn-hangzhou", "{your_access_key_id}", "{your_access_key_id}")
	if err != nil {
		panic(err)
	}

	request := requests.NewCommonRequest()
	request.Domain = "ecs.aliyuncs.com"
	request.Version = "2014-05-26"
	request.ApiName = "DescribeInstanceStatus"

	request.QueryParams["PageNumber"] = "1"
	request.QueryParams["PageSize"] = "30"

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		panic(err)
	}

	fmt.Print(response.GetHttpContentString())
}
```
