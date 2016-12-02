// Copyright 2016 Google Inc. All Rights Reserved.
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
#include "gtest/gtest.h"
#include "include/api_manager/utils/status.h"
#include "src/api_manager/service_control/proto.h"

namespace gasv1 = ::google::api::servicecontrol::v1;

using ::google::api::servicecontrol::v1::CheckError;
using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace service_control {

namespace {

Status ConvertCheckErrorToStatus(gasv1::CheckError::Code code,
                                 const char* error_detail,
                                 const char* service_name) {
  gasv1::CheckResponse response;
  gasv1::CheckError* check_error = response.add_check_errors();
  CheckRequestInfo info;
  check_error->set_code(code);
  check_error->set_detail(error_detail);
  return Proto::ConvertCheckResponse(response, service_name, nullptr);
}

Status ConvertCheckErrorToStatus(gasv1::CheckError::Code code) {
  gasv1::CheckResponse response;
  std::string service_name;
  response.add_check_errors()->set_code(code);
  return Proto::ConvertCheckResponse(response, service_name, nullptr);
}

}  // namespace

TEST(CheckResponseTest, AbortedWithInvalidArgumentWhenRespIsKeyInvalid) {
  Status result = ConvertCheckErrorToStatus(CheckError::API_KEY_INVALID);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(CheckResponseTest, AbortedWithInvalidArgumentWhenRespIsKeyExpired) {
  Status result = ConvertCheckErrorToStatus(CheckError::API_KEY_EXPIRED);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(CheckResponseTest,
     AbortedWithInvalidArgumentWhenRespIsBlockedWithNotFound) {
  Status result = ConvertCheckErrorToStatus(CheckError::NOT_FOUND);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(CheckResponseTest,
     AbortedWithInvalidArgumentWhenRespIsBlockedWithKeyNotFound) {
  Status result = ConvertCheckErrorToStatus(CheckError::API_KEY_NOT_FOUND);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithServiceNotActivated) {
  Status result = ConvertCheckErrorToStatus(
      CheckError::SERVICE_NOT_ACTIVATED, "Service not activated.", "api_xxxx");
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
  EXPECT_EQ(result.message(), "API api_xxxx is not enabled for the project.");
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithPermissionDenied) {
  Status result = ConvertCheckErrorToStatus(CheckError::PERMISSION_DENIED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithIpAddressBlocked) {
  Status result = ConvertCheckErrorToStatus(CheckError::IP_ADDRESS_BLOCKED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithRefererBlocked) {
  Status result = ConvertCheckErrorToStatus(CheckError::REFERER_BLOCKED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithClientAppBlocked) {
  Status result = ConvertCheckErrorToStatus(CheckError::CLIENT_APP_BLOCKED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenResponseIsBlockedWithProjectDeleted) {
  Status result = ConvertCheckErrorToStatus(CheckError::PROJECT_DELETED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenResponseIsBlockedWithProjectInvalid) {
  Status result = ConvertCheckErrorToStatus(CheckError::PROJECT_INVALID);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(CheckResponseTest,
     AbortedWithPermissionDeniedWhenResponseIsBlockedWithBillingDisabled) {
  Status result = ConvertCheckErrorToStatus(CheckError::BILLING_DISABLED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(CheckResponseTest, FailOpenWhenResponseIsUnknownNamespaceLookup) {
  EXPECT_TRUE(
      ConvertCheckErrorToStatus(CheckError::NAMESPACE_LOOKUP_UNAVAILABLE).ok());
}

TEST(CheckResponseTest, FailOpenWhenResponseIsUnknownBillingStatus) {
  EXPECT_TRUE(
      ConvertCheckErrorToStatus(CheckError::BILLING_STATUS_UNAVAILABLE).ok());
}

TEST(CheckResponseTest, FailOpenWhenResponseIsUnknownServiceStatus) {
  EXPECT_TRUE(
      ConvertCheckErrorToStatus(CheckError::SERVICE_STATUS_UNAVAILABLE).ok());
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
