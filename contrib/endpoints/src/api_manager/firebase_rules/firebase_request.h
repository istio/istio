// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////

#ifndef FIREBASE_RULES_FIREBASE_REQUEST_H_
#define FIREBASE_RULES_FIREBASE_REQUEST_H_

#include <string>
#include <utility>
#include <vector>
#include "contrib/endpoints/include/api_manager/utils/status.h"
#include "contrib/endpoints/src/api_manager/context/request_context.h"
#include "contrib/endpoints/src/api_manager/proto/security_rules.pb.h"

// An object of this class should be created for each RequestContext object.
// Here is the flow of messages between ESP, Firebase rules and User provided
// HTTP endpoint:
//
// 1) ESP invokes GetRelease API call on Firebase Service to get the ruleset
// name associated with the Release. The ruleset a representation of the rules
// files that the user deployed to be enforced. The release name is built by
// concatenating the service name and the api version number.
//
// 2) ESP receives the response from Firebase Service which contains the ruleset
// name associated with the Release. From this point on, ESP invokes TestRuleset
// request against this ruleset name.
//
// 3) ESP issues the TestRuleset request that includes the following
// information:
//  -- The payload of the JWT token which contains, uid, email or any additional
//  claims that can be used to authorize the user.
//  -- A test case that provides the Request's HTTP method and HTTP path.
// Note that the above information is provided for ALL TestRuleset requests.
//
// 4) ESP receives a response in TestRulesetResponse message which has a state
// variable that is either set to SUCCESS or FAILURE.
//  -- If the state is SUCCESS, then ESP considers this as authorization
//  approval and invokes the continution provided with Status::OK.
//  -- If the state is FAILURE, then ESP looks more into the TestRulesetResponse
//  message to see if there are any user defined HTTP requests that are to be
//  invoked and ESP has not seen this request before. If there are no such
//  functions, then ESP stops processing with Unauthorized Access error.
//  Otherwise, ESP does Step 5.
//
// 5) ESP invokes rules defined HTTP requests that are not yet seen.
// ESP invokes these requests sequentially. Once all HTTP requests are invoked,
// then ESP builds a TestRulesetRequest which contains the following in addition
// to the JWT claims and HTTP method and HTTP path.
// -- For each HTTP request, ESP converts the JSON object into a protobuf::Value
// and sets the result of the HTTP call that be accessed in the Firebase rules
// like a Map.
// ESP send the TestRuleset message to Firebase Service and processing moved to
// step 4) above.
namespace google {
namespace api_manager {
namespace firebase_rules {

// This structure models any HTTP request that is to be invoked. These include
// both the TestRuleset Request as well as the user defined requests.
struct HttpRequest {
  std::string url;
  std::string method;
  std::string body;
  auth::ServiceAccountToken::JWT_TOKEN_TYPE token_type;
  std::string audience;
};

// A FirebaseRequest object understands the various http requests that need
// to be generated as a part of the TestRuleset request and response cycle.
// Here is the intented use of this code:
// FirebaseRequest request(...);
// while(!request.is_done()) {
//  std::string url, method, body;
//
//  /* The following is not a valid C++ statement. But written so the reader can
//  get a general idea ... */
//
//  (url, method, body, token_type) = request.GetHttpRequest();
//  std::string body = InvokeHttpRequest(url, method, body,
//                                       GetToken(token_type));
//  updateResponse(body);
// }
//
// if (request.RequestStatus.ok()) {
//  .... ALLOW .....
// } else {
//  .... DENY .....
// }
class FirebaseRequest {
 public:
  // Constructor.
  FirebaseRequest(const std::string &ruleset_name, ApiManagerEnvInterface *env,
                  std::shared_ptr<context::RequestContext> context);

  // If the firebase Request calling can be terminated.
  bool is_done();

  // Get the request status. This request status is only valid if is_done is
  // true.
  utils::Status RequestStatus();

  // This call should be invoked to get the next http request to execute.
  HttpRequest GetHttpRequest();

  // The response for previous HttpRequest.
  void UpdateResponse(const std::string &body);

 private:
  utils::Status UpdateRulesetRequestBody(
      const ::google::protobuf::RepeatedPtrField<
          proto::TestRulesetResponse::TestResult::FunctionCall> &func_calls);
  utils::Status ProcessTestRulesetResponse(const std::string &body);
  utils::Status ProcessFunctionCallResponse(const std::string &body);
  utils::Status CheckFuncCallArgs(
      const proto::TestRulesetResponse::TestResult::FunctionCall &func);
  utils::Status AddFunctionMock(
      proto::TestRulesetRequest *request,
      const proto::TestRulesetResponse::TestResult::FunctionCall &func_call);
  void SetStatus(const utils::Status &status);
  utils::Status SetNextRequest();
  bool AllFunctionCallsProcessed();
  std::vector<std::pair<proto::TestRulesetResponse::TestResult::FunctionCall,
                        std::string>>::const_iterator
  Find(const proto::TestRulesetResponse::TestResult::FunctionCall &func_call);

  // The API manager environment. Primarily used for logging.
  ApiManagerEnvInterface *env_;

  // The request context for the current request in progress.
  std::shared_ptr<context::RequestContext> context_;

  // The test ruleset name which contains the firebase rules and is used to
  // invoke TestRuleset API.
  std::string ruleset_name_;

  // The Firebase server that supports the TestRuleset requests.
  std::string firebase_server_;

  // This variable tracks the status of the state machine.
  utils::Status current_status_;

  // Variable to track if the state machine is done processing. This is set to
  // true either when the processing is successfully done or when an error is
  // encountered and current_status_ is not Statu::OK anymore.
  bool is_done_;

  // The map is used to buffer the response for the user defined function calls.
  std::vector<std::pair<proto::TestRulesetResponse::TestResult::FunctionCall,
                        std::string>>
      funcs_with_result_;

  // The iterator iterates over the FunctionCalls the user wishes to invoke. So
  // long as this iterator is valid, the state machine issues HTTP requests to
  // the user defined HTTP endpoints. Once the iterator is equl to
  // func_call_iter.end(), then the TestRuleset is issued which includes the
  // function calls along with their responses.
  ::google::protobuf::RepeatedPtrField<
      proto::TestRulesetResponse::TestResult::FunctionCall>::const_iterator
      func_call_iter_;

  // The Test ruleset response currently being processed.
  proto::TestRulesetResponse response_;

  // This variable points to either firebase_http_request_ or
  // external_http_request_. This will allow the UpdateResponse method to
  // understand if the response received is for TestRuleset or user
  // defined HTTP endpoint. If next_request points to firebase_http_request_,
  // upon receiving a response, UpdateResponse will convert the response to
  // TestRulesetResponse and process the response. If next_request_ points
  // to external_http_request_, then the reponse provided via UpdateResponse
  // is converted into a protobuf::Value. This value is initialized to nullptr
  // and will be nullptr once is_done_ is set to true.
  HttpRequest *next_request_;

  // The HTTP request to be sent to firebase TestRuleset API
  HttpRequest firebase_http_request_;

  // The HTTP request invoked for user provided HTTP endpoint.
  HttpRequest external_http_request_;
};

}  // namespace firebase_rules
}  // namespace api_manager
}  // namespace google

#endif  // FIREBASE_RULES_REQUEST_HELPER_H_
