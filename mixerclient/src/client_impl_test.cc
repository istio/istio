/* Copyright 2017 Google Inc. All Rights Reserved.
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

#include <iostream>
#include "gmock/gmock.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::_;

namespace istio {
namespace mixer_client {
namespace {

const char kCheckRequest1[] = R"(
request_index: 1
attribute_update {
  string_attributes {
    key: 1
    value: "service_name"
  }
  string_attributes {
    key: 2
    value: "operation_name"
  }
  int64_attributes {
    key: 1
    value: 400
  }
}
)";

const char kReportRequest1[] = R"(
request_index: 1
attribute_update {
  string_attributes {
    key: 1
    value: "service_name"
  }
  string_attributes {
    key: 2
    value: "operation_name"
  }
  int64_attributes {
    key: 1
    value: 400
  }
}
)";

const char kQuotaRequest1[] = R"(
request_index: 1
attribute_update {
  string_attributes {
    key: 1
    value: "service_name"
  }
  string_attributes {
    key: 2
    value: "operation_name"
  }
  int64_attributes {
    key: 1
    value: 400
  }
}
)";
const char kSuccessCheckResponse1[] = R"(
request_index: 1
result {
  code: 200
}
)";

const char kReportResponse1[] = R"(
request_index: 1
result {
  code: 200
}
)";

const char kQuotaResponse1[] = R"(
request_index: 1
)";

const char kErrorCheckResponse1[] = R"()";

// A mocking class to mock CheckTransport interface.
class MockCheckTransport {
 public:
  MOCK_METHOD3(Check, void(const CheckRequest &, CheckResponse *, DoneFunc));

  TransportCheckFunc GetFunc() {
    return
        [this](const CheckRequest &request, CheckResponse *response,
               DoneFunc on_done) { this->Check(request, response, on_done); };
  }

  MockCheckTransport() : check_response_(NULL) {}

  ~MockCheckTransport() {}

  // The done callback is called right away (in place).
  void CheckWithInplaceCallback(const CheckRequest &request,
                                CheckResponse *response, DoneFunc on_done) {
    check_request_ = request;
    if (check_response_) {
      *response = *check_response_;
    }
    on_done(done_status_);
  }

  // Saved check_request from mocked Transport::Check() call.
  CheckRequest check_request_;
  // If not NULL, the check response to send for mocked Transport::Check() call.
  CheckResponse *check_response_;

  // The status to send in on_done call back for Check() or Report().
  Status done_status_;
};

// A mocking class to mock ReportTransport interface.
class MockReportTransport {
 public:
  MOCK_METHOD3(Report, void(const ReportRequest &, ReportResponse *, DoneFunc));

  TransportReportFunc GetFunc() {
    return
        [this](const ReportRequest &request, ReportResponse *response,
               DoneFunc on_done) { this->Report(request, response, on_done); };
  }

  MockReportTransport() : report_response_(NULL) {}

  ~MockReportTransport() {}

  // The done callback is called right away (in place).
  void ReportWithInplaceCallback(const ReportRequest &request,
                                 ReportResponse *response, DoneFunc on_done) {
    report_request_ = request;
    if (report_response_) {
      *response = *report_response_;
    }
    on_done(done_status_);
  }

  // Saved report_request from mocked Transport::Report() call.
  ReportRequest report_request_;
  // If not NULL, the report response to send for mocked Transport::Report()
  // call.
  ReportResponse *report_response_;

  // The status to send in on_done call back for Check() or Report().
  Status done_status_;
};

// A mocking class to mock QuotaTransport interface.
class MockQuotaTransport {
 public:
  MOCK_METHOD3(Quota, void(const QuotaRequest &, QuotaResponse *, DoneFunc));

  TransportQuotaFunc GetFunc() {
    return
        [this](const QuotaRequest &request, QuotaResponse *response,
               DoneFunc on_done) { this->Quota(request, response, on_done); };
  }

  MockQuotaTransport() : quota_response_(NULL) {}

  ~MockQuotaTransport() {}

  // The done callback is called right away (in place).
  void QuotaWithInplaceCallback(const QuotaRequest &request,
                                QuotaResponse *response, DoneFunc on_done) {
    quota_request_ = request;
    if (quota_response_) {
      *response = *quota_response_;
    }
    on_done(done_status_);
  }

  // Saved report_request from mocked Transport::Report() call.
  QuotaRequest quota_request_;
  // If not NULL, the report response to send for mocked Transport::Report()
  // call.
  QuotaResponse *quota_response_;

  // The status to send in on_done call back for Check() or Report().
  Status done_status_;
};
}  // namespace

class MixerClientImplTest : public ::testing::Test {
 public:
  void SetUp() {
    ASSERT_TRUE(TextFormat::ParseFromString(kCheckRequest1, &check_request1_));
    ASSERT_TRUE(TextFormat::ParseFromString(kSuccessCheckResponse1,
                                            &pass_check_response1_));
    ASSERT_TRUE(
        TextFormat::ParseFromString(kReportRequest1, &report_request1_));

    ASSERT_TRUE(TextFormat::ParseFromString(kQuotaRequest1, &quota_request1_));

    MixerClientOptions options(
        CheckOptions(1 /*entries */, 500 /* refresh_interval_ms */,
                     1000 /* expiration_ms */),
        ReportOptions(1 /* entries */, 500 /*flush_interval_ms*/),
        QuotaOptions(1 /* entries */, 500 /*flush_interval_ms*/,
                     1000 /* expiration_ms */));
    options.check_transport = mock_check_transport_.GetFunc();
    options.report_transport = mock_report_transport_.GetFunc();
    options.quota_transport = mock_quota_transport_.GetFunc();
    client_ = CreateMixerClient(options);
  }

  CheckRequest check_request1_;
  CheckResponse pass_check_response1_;
  CheckResponse error_check_response1_;

  ReportRequest report_request1_;
  QuotaRequest quota_request1_;

  MockCheckTransport mock_check_transport_;
  MockReportTransport mock_report_transport_;
  MockQuotaTransport mock_quota_transport_;

  std::unique_ptr<MixerClient> client_;
};

TEST_F(MixerClientImplTest, TestNonCachedCheckWithInplaceCallback) {
  // Calls a Client::Check, the request is not in the cache
  // Transport::Check() is called.  It will send a successful check response
  EXPECT_CALL(mock_check_transport_, Check(_, _, _))
      .WillOnce(Invoke(&mock_check_transport_,
                       &MockCheckTransport::CheckWithInplaceCallback));

  mock_check_transport_.done_status_ = Status::OK;

  CheckResponse check_response;
  Status done_status = Status::UNKNOWN;
  client_->Check(check_request1_, &check_response,
                 [&done_status](Status status) { done_status = status; });

  // Since it is not cached, transport should be called.
  EXPECT_TRUE(MessageDifferencer::Equals(mock_check_transport_.check_request_,
                                         check_request1_));
}

TEST_F(MixerClientImplTest, TestNonCachedReportWithInplaceCallback) {
  // Calls Client::Report, it will not be cached.
  // Transport::Report() should be called.
  // Transport::on_done() is called inside Transport::Report() with error
  // PERMISSION_DENIED. The Client::done_done() is called with the same error.
  EXPECT_CALL(mock_report_transport_, Report(_, _, _))
      .WillOnce(Invoke(&mock_report_transport_,
                       &MockReportTransport::ReportWithInplaceCallback));

  mock_report_transport_.done_status_ = Status(Code::PERMISSION_DENIED, "");

  ReportResponse report_response;
  Status done_status = Status::UNKNOWN;
  // This request is high important, so it will not be cached.
  // client->Report() will call Transport::Report() right away.
  client_->Report(report_request1_, &report_response,
                  [&done_status](Status status) { done_status = status; });

  // Since it is not cached, transport should be called.
  EXPECT_TRUE(MessageDifferencer::Equals(mock_report_transport_.report_request_,
                                         report_request1_));
}

TEST_F(MixerClientImplTest, TestNonCachedQuotaWithInplaceCallback) {
  // Calls Client::Quota with a high important request, it will not be cached.
  // Transport::Quota() should be called.
  EXPECT_CALL(mock_quota_transport_, Quota(_, _, _))
      .WillOnce(Invoke(&mock_quota_transport_,
                       &MockQuotaTransport::QuotaWithInplaceCallback));

  // Set the report status to be used in the on_report_done
  mock_report_transport_.done_status_ = Status::OK;

  QuotaResponse quota_response;
  Status done_status = Status::UNKNOWN;
  client_->Quota(quota_request1_, &quota_response,
                 [&done_status](Status status) { done_status = status; });

  // Since it is not cached, transport should be called.
  EXPECT_TRUE(MessageDifferencer::Equals(mock_quota_transport_.quota_request_,
                                         quota_request1_));
}

}  // namespace mixer_client
}  // namespace istio