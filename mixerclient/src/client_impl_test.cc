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

using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {

// A mocking class to mock CheckTransport interface.
class MockCheckTransport {
 public:
  MOCK_METHOD3(Check, void(const CheckRequest&, CheckResponse*, DoneFunc));
  TransportCheckFunc GetFunc() {
    return
        [this](const CheckRequest& request, CheckResponse* response,
               DoneFunc on_done) { this->Check(request, response, on_done); };
  }
};

class MixerClientImplTest : public ::testing::Test {
 public:
  MixerClientImplTest() {
    MixerClientOptions options(
        CheckOptions(1 /*entries */),
        QuotaOptions(1 /* entries */, 600000 /* expiration_ms */));
    options.check_options.network_fail_open = false;
    options.check_transport = mock_check_transport_.GetFunc();
    client_ = CreateMixerClient(options);
  }

  std::unique_ptr<MixerClient> client_;
  MockCheckTransport mock_check_transport_;
};

TEST_F(MixerClientImplTest, TestSuccessCheck) {
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillOnce(Invoke([](const CheckRequest& request, CheckResponse* response,
                          DoneFunc on_done) { on_done(Status::OK); }));

  Attributes attributes;
  Status done_status = Status::UNKNOWN;
  client_->Check(attributes,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());
}

}  // namespace mixer_client
}  // namespace istio
