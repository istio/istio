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
#include "contrib/endpoints/include/api_manager/utils/status.h"
#include "contrib/endpoints/src/api_manager/service_control/proto.h"
#include "gtest/gtest.h"

namespace gasv1 = ::google::api::servicecontrol::v1;

using ::google::api::servicecontrol::v1::QuotaError;
using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace service_control {

namespace {

Status ConvertAllocateQuotaErrorToStatus(gasv1::QuotaError::Code code,
                                         const char* error_detail,
                                         const char* service_name) {
  gasv1::AllocateQuotaResponse response;
  gasv1::QuotaError* quota_error = response.add_allocate_errors();
  QuotaRequestInfo info;
  quota_error->set_code(code);
  quota_error->set_description(error_detail);
  return Proto::ConvertAllocateQuotaResponse(response, service_name);
}

Status ConvertAllocateQuotaErrorToStatus(gasv1::QuotaError::Code code) {
  gasv1::AllocateQuotaResponse response;
  std::string service_name;
  response.add_allocate_errors()->set_code(code);
  return Proto::ConvertAllocateQuotaResponse(response, service_name);
}

}  // namespace

TEST(AllocateQuotaResponseTest,
     AbortedWithInvalidArgumentWhenRespIsKeyInvalid) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::API_KEY_INVALID);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithInvalidArgumentWhenRespIsKeyExpired) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::API_KEY_EXPIRED);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithInvalidArgumentWhenRespIsBlockedWithResourceExausted) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::RESOURCE_EXHAUSTED);
  EXPECT_EQ(Code::RESOURCE_EXHAUSTED, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithInvalidArgumentWhenRespIsBlockedWithProjectSuspended) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::PROJECT_SUSPENDED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithServiceNotEnabled) {
  Status result = ConvertAllocateQuotaErrorToStatus(
      QuotaError::SERVICE_NOT_ENABLED,
      "API api_xxxx is not enabled for the project.", "api_xxxx");
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
  EXPECT_EQ(result.message(), "API api_xxxx is not enabled for the project.");
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithBillingNotActivated) {
  Status result = ConvertAllocateQuotaErrorToStatus(
      QuotaError::BILLING_NOT_ACTIVE,
      "API api_xxxx has billing disabled. Please enable it..", "api_xxxx");
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
  EXPECT_EQ(result.message(),
            "API api_xxxx has billing disabled. Please enable it..");
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithIpAddressBlocked) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::IP_ADDRESS_BLOCKED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithRefererBlocked) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::REFERER_BLOCKED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithClientAppBlocked) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::CLIENT_APP_BLOCKED);
  EXPECT_EQ(Code::PERMISSION_DENIED, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenResponseIsBlockedWithProjectInvalid) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::PROJECT_INVALID);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithProjectDeleted) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::PROJECT_DELETED);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithApiKeyInvalid) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::API_KEY_INVALID);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(AllocateQuotaResponseTest,
     AbortedWithPermissionDeniedWhenRespIsBlockedWithApiKeyExpiread) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::API_KEY_EXPIRED);
  EXPECT_EQ(Code::INVALID_ARGUMENT, result.code());
}

TEST(AllocateQuotaResponseTest,
     AcceptOKWhenRespIsBlockedWithProjectStatusUnavailable) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::PROJECT_STATUS_UNVAILABLE);
  EXPECT_EQ(Code::OK, result.code());
}

TEST(AllocateQuotaResponseTest,
     AcceptOKWhenRespIsBlockedWithServiceStatusUnavailable) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::SERVICE_STATUS_UNAVAILABLE);
  EXPECT_EQ(Code::OK, result.code());
}

TEST(AllocateQuotaResponseTest,
     AcceptOKWhenRespIsBlockedWithBillingStatusUnavailable) {
  Status result =
      ConvertAllocateQuotaErrorToStatus(QuotaError::BILLING_STATUS_UNAVAILABLE);
  EXPECT_EQ(Code::OK, result.code());
}

TEST(AllocateQuotaResponseTest, FailOpenWhenResponseIsUnknownBillingStatus) {
  EXPECT_TRUE(
      ConvertAllocateQuotaErrorToStatus(QuotaError::BILLING_STATUS_UNAVAILABLE)
          .ok());
}

TEST(AllocateQuotaResponseTest, FailOpenWhenResponseIsUnknownServiceStatus) {
  EXPECT_TRUE(
      ConvertAllocateQuotaErrorToStatus(QuotaError::SERVICE_STATUS_UNAVAILABLE)
          .ok());
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
