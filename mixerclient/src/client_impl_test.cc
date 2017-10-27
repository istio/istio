/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "include/client.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "include/attributes_builder.h"
#include "utils/status_test_util.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {
namespace {

const std::string kRequestCount = "RequestCount";

// A mocking class to mock CheckTransport interface.
class MockCheckTransport {
 public:
  MOCK_METHOD3(Check, void(const CheckRequest&, CheckResponse*, DoneFunc));
  TransportCheckFunc GetFunc() {
    return [this](const CheckRequest& request, CheckResponse* response,
                  DoneFunc on_done) -> CancelFunc {
      Check(request, response, on_done);
      return nullptr;
    };
  }
};

class MixerClientImplTest : public ::testing::Test {
 public:
  MixerClientImplTest() {
    AttributesBuilder(&request_).AddString("quota.name", kRequestCount);

    CreateClient(true /* check_cache */, true /* quota_cache */);
  }

  void CreateClient(bool check_cache, bool quota_cache) {
    MixerClientOptions options(CheckOptions(check_cache ? 1 : 0 /*entries */),
                               ReportOptions(1, 1000),
                               QuotaOptions(quota_cache ? 1 : 0 /* entries */,
                                            600000 /* expiration_ms */));
    options.check_options.network_fail_open = false;
    options.check_transport = mock_check_transport_.GetFunc();
    client_ = CreateMixerClient(options);
  }

  Attributes request_;
  std::unique_ptr<MixerClient> client_;
  MockCheckTransport mock_check_transport_;
  TransportCheckFunc empty_transport_;
};

TEST_F(MixerClientImplTest, TestSuccessCheck) {
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillOnce(Invoke([](const CheckRequest& request, CheckResponse* response,
                          DoneFunc on_done) {
        response->mutable_precondition()->set_valid_use_count(1000);
        on_done(Status::OK);
      }));

  // Remove quota, not to test quota
  request_.mutable_attributes()->erase("quota.name");
  Status done_status = Status::UNKNOWN;
  client_->Check(request_, empty_transport_,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());

  for (int i = 0; i < 10; i++) {
    // Other calls should ba cached.
    Status done_status1 = Status::UNKNOWN;
    client_->Check(request_, empty_transport_,
                   [&done_status1](Status status) { done_status1 = status; });
    EXPECT_TRUE(done_status1.ok());
  }
}

TEST_F(MixerClientImplTest, TestPerRequestTransport) {
  // Global transport should not be called.
  EXPECT_CALL(mock_check_transport_, Check(_, _, _)).Times(0);

  // For local pre-request transport.
  MockCheckTransport local_check_transport;
  EXPECT_CALL(local_check_transport, Check(_, _, _))
      .WillOnce(Invoke([](const CheckRequest& request, CheckResponse* response,
                          DoneFunc on_done) {
        response->mutable_precondition()->set_valid_use_count(1000);
        on_done(Status::OK);
      }));

  // Remove quota, not to test quota
  request_.mutable_attributes()->erase("quota.name");
  Status done_status = Status::UNKNOWN;
  client_->Check(request_, local_check_transport.GetFunc(),
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());

  for (int i = 0; i < 10; i++) {
    // Other calls should ba cached.
    Status done_status1 = Status::UNKNOWN;
    client_->Check(request_, local_check_transport.GetFunc(),
                   [&done_status1](Status status) { done_status1 = status; });
    EXPECT_TRUE(done_status1.ok());
  }
}

TEST_F(MixerClientImplTest, TestNoCheckCache) {
  CreateClient(false /* check_cache */, true /* quota_cache */);

  int call_counts = 0;
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillRepeatedly(Invoke([&](const CheckRequest& request,
                                 CheckResponse* response, DoneFunc on_done) {
        response->mutable_precondition()->set_valid_use_count(1000);
        CheckResponse::QuotaResult quota_result;
        quota_result.set_granted_amount(10);
        quota_result.mutable_valid_duration()->set_seconds(10);
        (*response->mutable_quotas())[kRequestCount] = quota_result;
        call_counts++;
        on_done(Status::OK);
      }));

  Status done_status = Status::UNKNOWN;
  client_->Check(request_, empty_transport_,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());

  for (int i = 0; i < 10; i++) {
    // Other calls should ba cached.
    Status done_status1 = Status::UNKNOWN;
    client_->Check(request_, empty_transport_,
                   [&done_status1](Status status) { done_status1 = status; });
    EXPECT_TRUE(done_status1.ok());
  }
  // Call count 11 since check is not cached.
  EXPECT_LE(call_counts, 11);
}

TEST_F(MixerClientImplTest, TestNoQuotaCache) {
  CreateClient(true /* check_cache */, false /* quota_cache */);

  int call_counts = 0;
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillRepeatedly(Invoke([&](const CheckRequest& request,
                                 CheckResponse* response, DoneFunc on_done) {
        response->mutable_precondition()->set_valid_use_count(1000);
        CheckResponse::QuotaResult quota_result;
        quota_result.set_granted_amount(10);
        quota_result.mutable_valid_duration()->set_seconds(10);
        (*response->mutable_quotas())[kRequestCount] = quota_result;
        call_counts++;
        on_done(Status::OK);
      }));

  Status done_status = Status::UNKNOWN;
  client_->Check(request_, empty_transport_,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());

  for (int i = 0; i < 10; i++) {
    // Other calls should ba cached.
    Status done_status1 = Status::UNKNOWN;
    client_->Check(request_, empty_transport_,
                   [&done_status1](Status status) { done_status1 = status; });
    EXPECT_TRUE(done_status1.ok());
  }
  // Call count 11 since quota is not cached.
  EXPECT_LE(call_counts, 11);
}

TEST_F(MixerClientImplTest, TestSuccessCheckAndQuota) {
  int call_counts = 0;
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillRepeatedly(Invoke([&](const CheckRequest& request,
                                 CheckResponse* response, DoneFunc on_done) {
        response->mutable_precondition()->set_valid_use_count(1000);
        CheckResponse::QuotaResult quota_result;
        quota_result.set_granted_amount(10);
        quota_result.mutable_valid_duration()->set_seconds(10);
        (*response->mutable_quotas())[kRequestCount] = quota_result;
        call_counts++;
        on_done(Status::OK);
      }));

  Status done_status = Status::UNKNOWN;
  client_->Check(request_, empty_transport_,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());

  for (int i = 0; i < 10; i++) {
    // Other calls should ba cached.
    Status done_status1 = Status::UNKNOWN;
    client_->Check(request_, empty_transport_,
                   [&done_status1](Status status) { done_status1 = status; });
    EXPECT_TRUE(done_status1.ok());
  }
  // Call count should be less than 4
  EXPECT_LE(call_counts, 3);
}

TEST_F(MixerClientImplTest, TestFailedCheckAndQuota) {
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillOnce(Invoke([](const CheckRequest& request, CheckResponse* response,
                          DoneFunc on_done) {
        response->mutable_precondition()->mutable_status()->set_code(
            Code::FAILED_PRECONDITION);
        response->mutable_precondition()->set_valid_use_count(100);
        CheckResponse::QuotaResult quota_result;
        quota_result.set_granted_amount(10);
        quota_result.mutable_valid_duration()->set_seconds(10);
        (*response->mutable_quotas())[kRequestCount] = quota_result;
        on_done(Status::OK);
      }));

  Status done_status = Status::UNKNOWN;
  client_->Check(request_, empty_transport_,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_ERROR_CODE(Code::FAILED_PRECONDITION, done_status);

  for (int i = 0; i < 10; i++) {
    // Other calls should ba cached.
    Status done_status1 = Status::UNKNOWN;
    client_->Check(request_, empty_transport_,
                   [&done_status1](Status status) { done_status1 = status; });
    EXPECT_ERROR_CODE(Code::FAILED_PRECONDITION, done_status1);
  }
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
