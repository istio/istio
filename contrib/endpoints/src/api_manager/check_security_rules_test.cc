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
//

#include "contrib/endpoints/src/api_manager/check_security_rules.h"
#include "contrib/endpoints/src/api_manager/context/request_context.h"
#include "contrib/endpoints/src/api_manager/context/service_context.h"
#include "contrib/endpoints/src/api_manager/mock_api_manager_environment.h"
#include "contrib/endpoints/src/api_manager/mock_request.h"
#include "contrib/endpoints/src/api_manager/proto/security_rules.pb.h"
#include "contrib/endpoints/src/api_manager/utils/marshalling.h"
#include "google/protobuf/util/message_differencer.h"

using ::testing::_;
using ::testing::AllOf;
using ::testing::InSequence;
using ::testing::Invoke;
using ::testing::Property;
using ::testing::Return;
using ::testing::StrCaseEq;
using ::testing::StrEq;
using ::testing::StrNe;

using ::google::protobuf::util::MessageDifferencer;
using ::google::api_manager::utils::Status;
using ::google::protobuf::Map;
using ::google::protobuf::util::error::Code;
using ::google::protobuf::RepeatedPtrField;

// Tuple with arg<0> = function name
// arg<1> = url, arg<2> = method, arg<3> = body.
using FuncTuple =
    std::tuple<std::string, std::string, std::string, std::string, std::string>;
using ::google::api_manager::proto::TestRulesetResponse;
using FunctionCall = TestRulesetResponse::TestResult::FunctionCall;

namespace google {
namespace api_manager {

namespace {
// Service name to be used.
const char kServiceName[] = R"(
name: "myfirebaseapp.appspot.com"
)";

// Bad service name that will result in bad release name
const char kBadServiceName[] = R"(
name: "badService.appspot.com"
)";

// Producer project Id used to create the release URL
const char kProducerProjectId[] = R"(
producer_project_id: "myfirebaseapp"
)";

// APIs definition.
const char kApis[] = R"(
apis {
  name: "Bookstore"
  version: "v1"
  methods {
    name: "ListShelves"
    request_type_url: "types.googleapis.com/google.protobuf.Empty"
    response_type_url: "types.googleapis.com/Bookstore.ListShelvesResponse"
  }
  methods {
    name: "ListBooks"
    request_type_url: "types.googleapis.com/google.protobuf.Empty"
    response_type_url: "types.googleapis.com/Bookstore.ListBooksResponse"
  }
  methods {
    name: "CreateBook"
    request_type_url: "types.googleapis.com/Bookstore.CreateBookRequest"
    response_type_url: "types.googleapis.com/Bookstore.Book"
  }
})";

// Authentication part of service config
const char kAuthentication[] = R"(
authentication {
  providers {
    id: "issuer1"
    issuer: "https://issuer1.com"
  }
  providers {
    id: "issuer2"
    issuer: "https://issuer2.com"
    jwks_uri: "https://issuer2.com/pubkey"
  }
  rules {
    selector: "ListShelves"
    requirements {
      provider_id: "issuer1"
    }
    requirements {
      provider_id: "issuer2"
    }
  }
})";

// Http part of service config
const char kHttp[] = R"(
http {
  rules {
    selector: "ListShelves"
    get: "/ListShelves"
  }
}
control {
  environment: "http://127.0.0.1:8081"
})";

// Jwt payload to be used
const char kJwtEmailPayload[] =
    R"({"iss":"https://accounts.google.com","iat":1486575396,"exp":1486578996,"aud":"https://myfirebaseapp.appspot.com","sub":"113424383671131376652","email_verified":true,"azp":"limin-429@appspot.gserviceaccount.com","email":"limin-429@appspot.gserviceaccount.com"})";

// Firebase Server config
static const char kServerConfig[] = R"(
api_check_security_rules_config {
  firebase_server: "https://myfirebaseserver.com"
})";

// The response to GetRelease call to firebase server.
static const char kRelease[] = R"(
{
  "name": "projects/myfirebaseapp/releases/myfirebaseapp.appspot.com:v1",
  "rulesetName": "projects/myfirebaseapp/rulesets/99045fc0-a5e4-47e2-a665-f88593594b6b",
  "createTime": "2017-01-10T16:52:27.764111Z",
  "updateTime": "2017-01-10T16:52:27.764111Z"
})";

// Error response for GetRelease on bad release name
static const char kReleaseError[] = R"(
{
  "error": {
    "code": 404,
    "message": "Requested entity was not found.",
    "status": "NOT_FOUND",
  }
})";

static const char kDummyBody[] = R"(
{
  "key" : "value"
})";

static const char kDummyAudience[] = "test-audience";

const char kFirstRequest[] =
    R"({"testSuite":{"testCases":[{"expectation":"ALLOW","request":{"method":"get","path":"/ListShelves","auth":{"token":{"email":"limin-429@appspot.gserviceaccount.com","email_verified":true,"azp":"limin-429@appspot.gserviceaccount.com","aud":"https://myfirebaseapp.appspot.com","sub":"113424383671131376652","iat":1486575396,"iss":"https://accounts.google.com","exp":1486578996}}}}]}})";

const char kSecondRequest[] =
    R"({"testSuite":{"testCases":[{"expectation":"ALLOW","request":{"auth":{"token":{"email":"limin-429@appspot.gserviceaccount.com","azp":"limin-429@appspot.gserviceaccount.com","aud":"https://myfirebaseapp.appspot.com","sub":"113424383671131376652","iss":"https://accounts.google.com","email_verified":true,"iat":1486575396,"exp":1486578996}},"method":"get","path":"/ListShelves"},"functionMocks":[{"function":"f1","args":[{"exactValue":"http://url1"},{"exactValue":"POST"},{"exactValue":{"key":"value"}},{"exactValue":"test-audience"}],"result":{"value":{"key":"value"}}}]}]}})";

const char kThirdRequest[] =
    R"({"testSuite":{"testCases":[{"expectation":"ALLOW","request":{"method":"get","path":"/ListShelves","auth":{"token":{"email":"limin-429@appspot.gserviceaccount.com","iat":1486575396,"azp":"limin-429@appspot.gserviceaccount.com","exp":1486578996,"email_verified":true,"sub":"113424383671131376652","aud":"https://myfirebaseapp.appspot.com","iss":"https://accounts.google.com"}}},"functionMocks":[{"function":"f2","args":[{"exactValue":"http://url2"},{"exactValue":"GET"},{"exactValue":{"key":"value"}},{"exactValue":"test-audience"}],"result":{"value":{"key":"value"}}},{"function":"f3","args":[{"exactValue":"https://url3"},{"exactValue":"GET"},{"exactValue":{"key":"value"}},{"exactValue":"test-audience"}],"result":{"value":{"key":"value"}}},{"function":"f1","args":[{"exactValue":"http://url1"},{"exactValue":"POST"},{"exactValue":{"key":"value"}},{"exactValue":"test-audience"}],"result":{"value":{"key":"value"}}}]}]}})";

::google::protobuf::Value ToValue(const std::string &arg) {
  ::google::protobuf::Value value;
  value.set_string_value(arg);
  return value;
}

MATCHER_P3(HTTPRequestMatches, url, method, body, "") {
  if (arg->url() != url) {
    return false;
  }

  if (strcasecmp(method.c_str(), arg->method().c_str()) != 0) {
    return false;
  }

  if (body.empty() || arg->body().empty()) {
    return body.empty() && arg->body().empty();
  }

  google::protobuf::Value actual;
  google::protobuf::Value expected;

  if (utils::JsonToProto(body, &expected) != Status::OK ||
      utils::JsonToProto(arg->body(), &actual) != Status::OK) {
    return false;
  }

  return MessageDifferencer::Equals(actual, expected);
}

// Get service configuration with no authentication member field. This will
// disable auth and will also disable security rules check.
std::pair<std::string, std::string> GetConfigWithNoAuth() {
  std::string service_config = std::string(kServiceName) + kApis + kHttp;
  return std::make_pair(service_config, std::string(kServerConfig));
}

// Get Service configuration with no apis. This will result in the version field
// not present and should disable security rules check.
std::pair<std::string, std::string> GetConfigWithoutApis() {
  std::string service_config =
      std::string(kServiceName) + kAuthentication + kHttp;

  return std::make_pair(service_config, std::string(kServerConfig));
}

// There is no firebase server configuration.
std::pair<std::string, std::string> GetConfigWithoutServer() {
  std::string service_config =
      std::string(kServiceName) + kApis + kAuthentication + kHttp;
  return std::make_pair(service_config, "");
}

// This test class is parameterized and creates Config object based on the
// service and server configuration provided.
class CheckDisableSecurityRulesTest
    : public ::testing::TestWithParam<std::pair<std::string, std::string>> {
 public:
  void SetUp() {
    std::unique_ptr<MockApiManagerEnvironment> env(
        new ::testing::NiceMock<MockApiManagerEnvironment>());
    MockApiManagerEnvironment *raw_env = env.get();

    std::string service_config;
    std::string server_config;

    std::tie(service_config, server_config) = GetParam();
    std::unique_ptr<Config> config = Config::Create(raw_env, service_config);

    ASSERT_TRUE(config != nullptr);

    service_context_ = std::make_shared<context::ServiceContext>(
        std::move(env), server_config, std::move(config));

    ASSERT_TRUE(service_context_.get() != nullptr);

    std::unique_ptr<MockRequest> request(
        new ::testing::NiceMock<MockRequest>());

    request_context_ = std::make_shared<context::RequestContext>(
        service_context_, std::move(request));
    EXPECT_CALL(*raw_env, DoRunHTTPRequest(_)).Times(0);
  }

  std::shared_ptr<context::RequestContext> request_context_;
  std::shared_ptr<context::ServiceContext> service_context_;
};

// Paramterized test that will check for various configurations that will
// disable auth.
TEST_P(CheckDisableSecurityRulesTest, CheckAuthzDisabled) {
  CheckSecurityRules(request_context_,
                     [](Status status) { ASSERT_TRUE(status.ok()); });
}

// Invoke the tests on CheckDisableSecurityRulesTest with various parameters.
INSTANTIATE_TEST_CASE_P(ConfigToDisableFirebaseRulesCheck,
                        CheckDisableSecurityRulesTest,
                        testing::Values(GetConfigWithNoAuth(),
                                        GetConfigWithoutApis(),
                                        GetConfigWithoutServer()));

// Class that sets up the required objects to test various scenarios.
class CheckSecurityRulesTest : public ::testing::Test {
 public:
  void SetUp(std::string service_config, std::string server_config) {
    std::unique_ptr<MockApiManagerEnvironment> env(
        new ::testing::NiceMock<MockApiManagerEnvironment>());
    raw_env_ = env.get();

    std::unique_ptr<Config> config = Config::Create(raw_env_, service_config);
    ASSERT_TRUE(config != nullptr);

    service_context_ = std::make_shared<context::ServiceContext>(
        std::move(env), server_config, std::move(config));

    ASSERT_TRUE(service_context_.get() != nullptr);

    std::unique_ptr<MockRequest> request(
        new ::testing::NiceMock<MockRequest>());
    raw_request_ = request.get();

    ON_CALL(*raw_request_, GetRequestHTTPMethod())
        .WillByDefault(Return(std::string("GET")));

    ON_CALL(*raw_request_, GetUnparsedRequestPath())
        .WillByDefault(Return(std::string("/ListShelves")));

    ON_CALL(*raw_request_, GetRequestPath())
        .WillByDefault(Return(std::string("/ListShelves")));

    request_context_ = std::make_shared<context::RequestContext>(
        service_context_, std::move(request));
    release_url_ =
        "https://myfirebaseserver.com/v1/projects/myfirebaseapp/"
        "releases/myfirebaseapp.appspot.com:v1";

    ruleset_test_url_ =
        "https://myfirebaseserver.com/v1"
        "/projects/myfirebaseapp/rulesets/99045fc0-a5e4-47e2-a665-f88593594b6b"
        ":test?alt=json";
  }

  void ExpectCall(std::string url, std::string method, std::string body,
                  std::string response, Status status = Status::OK) {
    EXPECT_CALL(*raw_env_,
                DoRunHTTPRequest(HTTPRequestMatches(url, method, body)))
        .WillOnce(Invoke([response, status](HTTPRequest *req) {
          std::map<std::string, std::string> empty;
          std::string body(response);
          req->OnComplete(status, std::move(empty), std::move(body));
        }));
  }

  std::string BuildTestRulesetResponse(
      bool isSuccess, std::vector<FuncTuple> funcs = std::vector<FuncTuple>()) {
    TestRulesetResponse response;
    auto *result = response.add_test_results();
    result->set_state(isSuccess ? TestRulesetResponse::TestResult::SUCCESS
                                : TestRulesetResponse::TestResult::FAILURE);

    std::string url, method, body;
    for (auto http : funcs) {
      auto *func = result->add_function_calls();
      func->set_function(std::get<0>(http));
      if (!std::get<1>(http).empty()) {
        func->add_args()->set_string_value(std::get<1>(http));
      }

      if (!std::get<2>(http).empty()) {
        func->add_args()->set_string_value(std::get<2>(http));
      }

      if (!std::get<3>(http).empty()) {
        ::google::protobuf::Value body;
        Status status = utils::JsonToProto(std::get<3>(http), &body);
        *(func->add_args()) = body;
      }

      if (!std::get<4>(http).empty()) {
        func->add_args()->set_string_value(std::get<4>(http));
      }
    }

    std::string json_str;
    utils::ProtoToJson(response, &json_str, utils::JsonOptions::DEFAULT);
    return json_str;
  }

  void SetProtoValue(const std::string &key,
                     const ::google::protobuf::Value &value,
                     ::google::protobuf::Value *head) {
    ::google::protobuf::Struct *s = head->mutable_struct_value();
    auto *fields = s->mutable_fields();
    (*fields)[key] = value;
  }

  MockApiManagerEnvironment *raw_env_;
  MockRequest *raw_request_;
  std::shared_ptr<context::RequestContext> request_context_;
  std::shared_ptr<context::ServiceContext> service_context_;
  std::string release_url_;
  std::string ruleset_test_url_;
};

// If the release name is bad, then check the following:
// 1. Ensure that GetRuleset request is inovked on bad release name.
// 2. In this case return Status with NOT_FOUND
// 3. Ensure that there are no more HTTP calls made to firbase TestRuleset
TEST_F(CheckSecurityRulesTest, CheckAuthzFailGetRelease) {
  std::string service_config = std::string(kBadServiceName) +
                               kProducerProjectId + kApis + kAuthentication +
                               kHttp;
  std::string server_config = kServerConfig;
  SetUp(service_config, server_config);

  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(AllOf(
                             Property(&HTTPRequest::url, StrNe(release_url_)),
                             Property(&HTTPRequest::method, StrCaseEq("GET")))))
      .WillOnce(Invoke([](HTTPRequest *req) {

        std::map<std::string, std::string> empty;
        std::string body(kReleaseError);
        req->OnComplete(
            Status(Code::NOT_FOUND, "Requested entity was not found"),
            std::move(empty), std::move(body));

      }));

  EXPECT_CALL(*raw_env_,
              DoRunHTTPRequest(
                  AllOf(Property(&HTTPRequest::url, StrEq(ruleset_test_url_)),
                        Property(&HTTPRequest::method, StrCaseEq("POST")))))
      .Times(0);

  auto ptr = this;
  CheckSecurityRules(request_context_,
                     [ptr](Status status) { ASSERT_TRUE(!status.ok()); });
}

// Check that the right status is returned when TestRuleset completes with an
// error. This is modelled as an internal error.
// 1. Ensure that GetRelease is invoked on the correct release url and correct
// method.
// 2. The mock will respond with the ruleset Id.
// 3. Ensure that TestRuleset is invoked on the the righ URl and method.
// 4. In this case, mock will return an INTERNAL ERROR.
// 5. Make sure that status is not OK in this case.
TEST_F(CheckSecurityRulesTest, CheckAuthzFailTestRuleset) {
  std::string service_config = std::string(kServiceName) + kProducerProjectId +
                               kApis + kAuthentication + kHttp;
  std::string server_config = kServerConfig;
  SetUp(service_config, server_config);

  request_context_->set_auth_claims(kJwtEmailPayload);
  ExpectCall(release_url_, "GET", "", kRelease);
  ExpectCall(ruleset_test_url_, "POST", kFirstRequest, "",
             Status(Code::INTERNAL, "Cannot talk to server"));

  CheckSecurityRules(request_context_, [](Status status) {
    ASSERT_TRUE(status.CanonicalCode() == Code::INTERNAL);
  });
}

// Check behavior when TestResultset return a "FAILURE" message.
// 1. Ensure GetRelease is invoked properly and in this case mock responds with
// the ruelset Id.
// 2. Ensure that the TestResultset is invoked correctly and respond wit ha
// Status::OK but with Failure body.
// 3. Asser that the final status returned is PERMISSION DENIED.
TEST_F(CheckSecurityRulesTest, CheckAuthzFailWithTestResultFailure) {
  std::string service_config = std::string(kServiceName) + kProducerProjectId +
                               kApis + kAuthentication + kHttp;
  std::string server_config = kServerConfig;
  SetUp(service_config, server_config);

  request_context_->set_auth_claims(kJwtEmailPayload);
  ExpectCall(release_url_, "GET", "", kRelease);

  ExpectCall(ruleset_test_url_, "POST", kFirstRequest,
             BuildTestRulesetResponse(false));

  CheckSecurityRules(request_context_, [](Status status) {
    ASSERT_TRUE(status.CanonicalCode() == Code::PERMISSION_DENIED);
  });
}

TEST_F(CheckSecurityRulesTest, CheckAuthzFailWithEmptyTestRulesetResponse) {
  std::string service_config = std::string(kServiceName) + kProducerProjectId +
                               kApis + kAuthentication + kHttp;
  std::string server_config = kServerConfig;
  SetUp(service_config, server_config);

  request_context_->set_auth_claims(kJwtEmailPayload);
  ExpectCall(release_url_, "GET", "", kRelease);

  ExpectCall(ruleset_test_url_, "POST", kFirstRequest, "{}");
  CheckSecurityRules(request_context_, [](Status status) {
    ASSERT_TRUE(status.CanonicalCode() == Code::INTERNAL);
  });
}

// Check for success case.
// 1. Ensure GetRelease is invoked properly and in this case mock responds with
// the ruelset Id.
// 2. Ensure that the TestResultset is invoked correctly and respond wit ha
// Status::OK but with SUCCESS body.
// 3. Asser that the final status returned is OK.
TEST_F(CheckSecurityRulesTest, CheckAuthzSuccess) {
  std::string service_config = std::string(kServiceName) + kProducerProjectId +
                               kApis + kAuthentication + kHttp;
  std::string server_config = kServerConfig;
  SetUp(service_config, server_config);

  request_context_->set_auth_claims(kJwtEmailPayload);
  ExpectCall(release_url_, "GET", "", kRelease);
  ExpectCall(ruleset_test_url_, "POST", kFirstRequest,
             BuildTestRulesetResponse(true));

  CheckSecurityRules(request_context_,
                     [](Status status) { ASSERT_TRUE(status.ok()); });
}

class CheckSecurityRulesFunctions : public CheckSecurityRulesTest,
                                    public ::testing::WithParamInterface<bool> {
 public:
  void SetUp() {
    std::string service_config = std::string(kServiceName) +
                                 kProducerProjectId + kApis + kAuthentication +
                                 kHttp;
    std::string server_config = kServerConfig;
    CheckSecurityRulesTest::SetUp(service_config, server_config);
    request_context_->set_auth_claims(kJwtEmailPayload);

    InSequence s;

    ExpectCall(release_url_, "GET", "", kRelease);
    ExpectCall(ruleset_test_url_, "POST", kFirstRequest,
               BuildTestRulesetResponse(
                   false, {std::make_tuple("f1", "http://url1", "POST",
                                           kDummyBody, kDummyAudience)}));

    ExpectCall("http://url1", "POST", kDummyBody, kDummyBody);
    ExpectCall(ruleset_test_url_, "POST", kSecondRequest,
               BuildTestRulesetResponse(
                   false, {std::make_tuple("f2", "http://url2", "GET",
                                           kDummyBody, kDummyAudience),
                           std::make_tuple("f3", "https://url3", "GET",
                                           kDummyBody, kDummyAudience),
                           std::make_tuple("f1", "http://url1", "POST",
                                           kDummyBody, kDummyAudience)}));
    ExpectCall("http://url2", "GET", kDummyBody, kDummyBody);
    ExpectCall("https://url3", "GET", kDummyBody, kDummyBody);
    ExpectCall(ruleset_test_url_, "POST", kThirdRequest,
               BuildTestRulesetResponse(
                   GetParam(), {std::make_tuple("f2", "http://url2", "GET",
                                                kDummyBody, kDummyAudience),
                                std::make_tuple("f3", "https://url3", "GET",
                                                kDummyBody, kDummyAudience),
                                std::make_tuple("f1", "http://url1", "POST",
                                                kDummyBody, kDummyAudience)}));
  }
};

// Check the function call request response loop:
// 1. ESP Send TestRulesetRequest (No function calls)
// 2. ESP gets TestRulesetResponse with f1("http://url1, "POST", kDummyBody)
// 3. ESP Send HTTP request to f1.url
// 4. ESP Send TestRulesetRequest with (f1.url, "POST", kDummyBody,
//                                      kDummyResponse);
// 5. EST gets TestRulesetResponse with f1, f2, f3
// 6. ESP Send HTTP request to f2.url and f3.url. (checks f1 response is
// buffered).
// 7. ESP Send TestRulesetRequest with (f1, f2, f3)
// 8. ESP receives TestRulesetRequest
TEST_P(CheckSecurityRulesFunctions, CheckMultipleFunctions) {
  auto ptr = this;
  auto success = GetParam();
  CheckSecurityRules(request_context_, [ptr, success](Status status) {
    if (success) {
      ASSERT_TRUE(status.ok()) << status.ToString();
    } else {
      ASSERT_TRUE(status.CanonicalCode() == Code::PERMISSION_DENIED)
          << status.ToString();
    }
  });
}

INSTANTIATE_TEST_CASE_P(CheckMultipleFunctionSuccessFailure,
                        CheckSecurityRulesFunctions, ::testing::Bool());

class CheckSecurityRulesBadFunctions
    : public CheckSecurityRulesTest,
      public ::testing::WithParamInterface<FuncTuple> {
 public:
  void SetUp() {
    std::string service_config = std::string(kServiceName) +
                                 kProducerProjectId + kApis + kAuthentication +
                                 kHttp;
    std::string server_config = kServerConfig;
    CheckSecurityRulesTest::SetUp(service_config, server_config);
    request_context_->set_auth_claims(kJwtEmailPayload);

    InSequence s;

    ExpectCall(release_url_, "GET", "", kRelease);
    ExpectCall(ruleset_test_url_, "POST", kFirstRequest,
               BuildTestRulesetResponse(false, {GetParam()}));

    EXPECT_CALL(*raw_env_,
                DoRunHTTPRequest(AllOf(
                    Property(&HTTPRequest::url, StrEq(std::get<1>(GetParam()))),
                    Property(&HTTPRequest::method,
                             StrCaseEq(std::get<2>(GetParam()))))))
        .Times(0);
  }
};

TEST_P(CheckSecurityRulesBadFunctions, CheckBadFunctionArguments) {
  auto ptr = this;
  CheckSecurityRules(request_context_, [ptr](Status status) {
    ASSERT_TRUE(status.CanonicalCode() == Code::INVALID_ARGUMENT)
        << status.ToString();
  });
}

INSTANTIATE_TEST_CASE_P(
    CheckSecurityRulesBadFunctionArguments, CheckSecurityRulesBadFunctions,
    ::testing::Values(
        // Empty function name
        std::make_tuple("", "http://url1", "POST", kDummyBody, kDummyAudience),
        // Argument count less than 3
        std::make_tuple("f1", "http://url1", "", "", kDummyAudience),
        // The url is not set
        std::make_tuple("f1", "", "POST", kDummyBody, kDummyAudience),
        // The url is not a http or https protocol
        std::make_tuple("f1", "ftp://url1", "POST", kDummyBody, kDummyAudience),
        // The audience is not present
        std::make_tuple("f1", "http://url1", "GET", kDummyBody, "")));
}
}  // namespace api_manager
}  // namespace google
