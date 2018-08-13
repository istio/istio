package ots

//Licensed under the Apache License, Version 2.0 (the "License");
//you may not use this file except in compliance with the License.
//You may obtain a copy of the License at
//
//http://www.apache.org/licenses/LICENSE-2.0
//
//Unless required by applicable law or agreed to in writing, software
//distributed under the License is distributed on an "AS IS" BASIS,
//WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//See the License for the specific language governing permissions and
//limitations under the License.

const ApiVersion = "v1"

type CreateTriggerRequestBody struct {
	RoleArn     string
	TriggerName string
	UdfInfo     *UdfInfo
}

type TriggerResponseBase struct {
	Code    string
	Message string
}

type CreateTriggerResponseBody struct {
	TriggerResponseBase
	Etag string
}

type GetTriggerResponseBody struct {
	TriggerResponseBase
	Trigger *TriggerInfo
}

type ListTriggerResponseBody struct {
	TriggerResponseBase
	Triggers []*TriggerInfo
}

type UdfInfo struct {
	ServiceName  string
	FunctionName string
}

type TriggerInfo struct {
	TriggerName  string
	AssumeRole   string
	CreateTime   int64
	InstanceName string
	DataTable    string
	UdfInfo      *UdfInfo
	Etag         string
	TimerId      string
}
