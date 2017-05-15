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
#include "contrib/endpoints/src/api_manager/check_security_rules.h"
#include <iostream>
#include <sstream>
#include "contrib/endpoints/src/api_manager/auth/lib/json_util.h"
#include "contrib/endpoints/src/api_manager/firebase_rules/firebase_request.h"
#include "contrib/endpoints/src/api_manager/utils/marshalling.h"

using ::google::api_manager::auth::GetStringValue;
using ::google::api_manager::firebase_rules::FirebaseRequest;
using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {
namespace {

const std::string kFailedFirebaseReleaseFetch =
    "Failed to fetch Firebase Release";
const std::string kFailedFirebaseTest = "Failed to execute Firebase Test";
const std::string kInvalidResponse =
    "Invalid JSON response from Firebase Service";
const std::string kV1 = "/v1";
const std::string kHttpGetMethod = "GET";
const std::string kProjects = "/projects";
const std::string kReleases = "/releases";
const std::string kRulesetName = "rulesetName";
const std::string kContentType = "Content-Type";
const std::string kApplication = "application/json";

std::string GetReleaseName(const context::RequestContext &context) {
  return context.service_context()->service_name() + ":" +
         context.service_context()->service().apis(0).version();
}

std::string GetReleaseUrl(const context::RequestContext &context) {
  return context.service_context()->config()->GetFirebaseServer() + kV1 +
         kProjects + "/" + context.service_context()->project_id() + kReleases +
         "/" + GetReleaseName(context);
}

// An AuthzChecker object is created for every incoming request. It does
// authorizaiton by calling Firebase Rules service.
class AuthzChecker : public std::enable_shared_from_this<AuthzChecker> {
 public:
  // Constructor
  AuthzChecker(ApiManagerEnvInterface *env,
               auth::ServiceAccountToken *sa_token);

  // Check for Authorization success or failure
  void Check(std::shared_ptr<context::RequestContext> context,
             std::function<void(Status status)> continuation);

 private:
  // This method invokes the Firebase TestRuleset API endpoint as well as user
  // defined endpoints provided by the TestRulesetResponse.
  void CallNextRequest(std::function<void(Status status)> continuation);

  // Parse the response for GET RELEASE API call
  Status ParseReleaseResponse(const std::string &json_str,
                              std::string *ruleset_id);

  // Invoke the HTTP call
  void HttpFetch(const std::string &url, const std::string &method,
                 const std::string &request_body,
                 auth::ServiceAccountToken::JWT_TOKEN_TYPE token_type,
                 const std::string &audience,
                 std::function<void(Status, std::string &&)> continuation);

  std::shared_ptr<AuthzChecker> GetPtr() { return shared_from_this(); }

  ApiManagerEnvInterface *env_;
  auth::ServiceAccountToken *sa_token_;
  std::unique_ptr<FirebaseRequest> request_handler_;
};

AuthzChecker::AuthzChecker(ApiManagerEnvInterface *env,
                           auth::ServiceAccountToken *sa_token)
    : env_(env), sa_token_(sa_token) {}

void AuthzChecker::Check(
    std::shared_ptr<context::RequestContext> context,
    std::function<void(Status status)> final_continuation) {
  // TODO: Check service config to see if "useSecurityRules" is specified.
  // If so, call Firebase Rules service TestRuleset API.

  if (!context->service_context()->IsRulesCheckEnabled() ||
      context->method() == nullptr || !context->method()->auth()) {
    env_->LogInfo("Skipping Firebase Rules checks since it is disabled.");
    final_continuation(Status::OK);
    return;
  }

  // Fetch the Release attributes and get ruleset name.
  auto checker = GetPtr();
  HttpFetch(GetReleaseUrl(*context), kHttpGetMethod, "",
            auth::ServiceAccountToken::JWT_TOKEN_FOR_FIREBASE,
            context->service_context()->config()->GetFirebaseAudience(),
            [context, final_continuation, checker](Status status,
                                                   std::string &&body) {
              std::string ruleset_id;
              if (status.ok()) {
                checker->env_->LogDebug(
                    std::string("GetReleasName succeeded with ") + body);
                status = checker->ParseReleaseResponse(body, &ruleset_id);
              } else {
                checker->env_->LogError(std::string("GetReleaseName for ") +
                                        GetReleaseUrl(*context.get()) +
                                        " with status " + status.ToString());
                status = Status(Code::INTERNAL, kFailedFirebaseReleaseFetch);
              }

              // If the parsing of the release body is successful, then call the
              // Test Api for firebase rules service.
              if (status.ok()) {
                checker->request_handler_ = std::unique_ptr<FirebaseRequest>(
                    new FirebaseRequest(ruleset_id, checker->env_, context));
                checker->CallNextRequest(final_continuation);
              } else {
                final_continuation(status);
              }
            });
}

void AuthzChecker::CallNextRequest(
    std::function<void(Status status)> continuation) {
  if (request_handler_->is_done()) {
    continuation(request_handler_->RequestStatus());
    return;
  }

  auto checker = GetPtr();
  firebase_rules::HttpRequest http_request = request_handler_->GetHttpRequest();
  HttpFetch(http_request.url, http_request.method, http_request.body,
            http_request.token_type, http_request.audience,
            [continuation, checker](Status status, std::string &&body) {

              checker->env_->LogError(std::string("Response Body = ") + body);
              if (status.ok() && !body.empty()) {
                checker->request_handler_->UpdateResponse(body);
                checker->CallNextRequest(continuation);
              } else {
                checker->env_->LogError(
                    std::string("Test API failed with ") +
                    (status.ok() ? "Empty Response" : status.ToString()));
                status = Status(Code::INTERNAL, kFailedFirebaseTest);
                continuation(status);
              }
            });
}

Status AuthzChecker::ParseReleaseResponse(const std::string &json_str,
                                          std::string *ruleset_id) {
  grpc_json *json = grpc_json_parse_string_with_len(
      const_cast<char *>(json_str.data()), json_str.length());

  if (!json) {
    return Status(Code::INVALID_ARGUMENT, kInvalidResponse);
  }

  Status status = Status::OK;
  const char *id = GetStringValue(json, kRulesetName.c_str());
  *ruleset_id = (id == nullptr) ? "" : id;

  if (ruleset_id->empty()) {
    env_->LogError("Empty ruleset Id received from firebase service");
    status = Status(Code::INTERNAL, kInvalidResponse);
  } else {
    env_->LogDebug(std::string("Received ruleset Id: ") + *ruleset_id);
  }

  grpc_json_destroy(json);
  return status;
}

void AuthzChecker::HttpFetch(
    const std::string &url, const std::string &method,
    const std::string &request_body,
    auth::ServiceAccountToken::JWT_TOKEN_TYPE token_type,
    const std::string &audience,
    std::function<void(Status, std::string &&)> continuation) {
  env_->LogDebug(std::string("Issue HTTP Request to url :") + url +
                 " method : " + method + " body: " + request_body);

  std::unique_ptr<HTTPRequest> request(new HTTPRequest([continuation](
      Status status, std::map<std::string, std::string> &&,
      std::string &&body) { continuation(status, std::move(body)); }));

  if (!request) {
    continuation(Status(Code::INTERNAL, "Out of memory"), "");
    return;
  }

  request->set_method(method).set_url(url).set_auth_token(
      sa_token_->GetAuthToken(token_type, audience));

  if (!request_body.empty()) {
    request->set_header(kContentType, kApplication).set_body(request_body);
  }

  env_->RunHTTPRequest(std::move(request));
}

}  // namespace

void CheckSecurityRules(std::shared_ptr<context::RequestContext> context,
                        std::function<void(Status status)> continuation) {
  std::shared_ptr<AuthzChecker> checker = std::make_shared<AuthzChecker>(
      context->service_context()->env(),
      context->service_context()->service_account_token());
  checker->Check(context, continuation);
}

}  // namespace api_manager
}  // namespace google
