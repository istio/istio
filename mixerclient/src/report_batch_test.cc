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

#include "src/report_batch.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "include/attributes_builder.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {

// A mocking class to mock ReportTransport interface.
class MockReportTransport {
 public:
  MOCK_METHOD3(Report, void(const ReportRequest&, ReportResponse*, DoneFunc));
  TransportReportFunc GetFunc() {
    return [this](const ReportRequest& request, ReportResponse* response,
                  DoneFunc on_done) -> CancelFunc {
      Report(request, response, on_done);
      return nullptr;
    };
  }
};

class MockTimer : public Timer {
 public:
  void Stop() override {}
  void Start(int interval_ms) override {}
  std::function<void()> cb_;
};

class ReportBatchTest : public ::testing::Test {
 public:
  ReportBatchTest() : mock_timer_(nullptr), compressor_({}) {
    batch_.reset(new ReportBatch(ReportOptions(3, 1000),
                                 mock_report_transport_.GetFunc(),
                                 GetTimerFunc(), compressor_));
  }

  TimerCreateFunc GetTimerFunc() {
    return [this](std::function<void()> cb) -> std::unique_ptr<Timer> {
      mock_timer_ = new MockTimer;
      mock_timer_->cb_ = cb;
      return std::unique_ptr<Timer>(mock_timer_);
    };
  }

  MockReportTransport mock_report_transport_;
  MockTimer* mock_timer_;
  AttributeCompressor compressor_;
  std::unique_ptr<ReportBatch> batch_;
};

TEST_F(ReportBatchTest, TestBatchDisabled) {
  // max_batch_entries = 0 or 1 to disable batch
  batch_.reset(new ReportBatch(ReportOptions(1, 1000),
                               mock_report_transport_.GetFunc(), nullptr,
                               compressor_));

  // Expect report transport to be called.
  EXPECT_CALL(mock_report_transport_, Report(_, _, _))
      .WillOnce(
          Invoke([](const ReportRequest& request, ReportResponse* response,
                    DoneFunc on_done) { on_done(Status::OK); }));

  Attributes report;
  batch_->Report(report);
}

TEST_F(ReportBatchTest, TestBatchReport) {
  int report_call_count = 0;
  EXPECT_CALL(mock_report_transport_, Report(_, _, _))
      .WillRepeatedly(Invoke([&](const ReportRequest& request,
                                 ReportResponse* response, DoneFunc on_done) {
        report_call_count++;
        on_done(Status::OK);
      }));

  Attributes report;
  for (int i = 0; i < 10; ++i) {
    batch_->Report(report);
  }
  EXPECT_EQ(report_call_count, 3);

  batch_->Flush();
  EXPECT_EQ(report_call_count, 4);
}

TEST_F(ReportBatchTest, TestNoDeltaUpdate) {
  int report_call_count = 0;
  EXPECT_CALL(mock_report_transport_, Report(_, _, _))
      .WillRepeatedly(Invoke([&](const ReportRequest& request,
                                 ReportResponse* response, DoneFunc on_done) {
        report_call_count++;
        on_done(Status::OK);
      }));

  Attributes report;
  AttributesBuilder(&report).AddString("key", "value");
  batch_->Report(report);
  EXPECT_EQ(report_call_count, 0);

  // Erase a key, so delta update fail to push the batched result.
  report.mutable_attributes()->erase("key");
  batch_->Report(report);
  EXPECT_EQ(report_call_count, 1);

  batch_->Flush();
  EXPECT_EQ(report_call_count, 2);
}

TEST_F(ReportBatchTest, TestBatchReportWithTimeout) {
  int report_call_count = 0;
  EXPECT_CALL(mock_report_transport_, Report(_, _, _))
      .WillRepeatedly(Invoke([&](const ReportRequest& request,
                                 ReportResponse* response, DoneFunc on_done) {
        report_call_count++;
        on_done(Status::OK);
      }));

  Attributes report;
  batch_->Report(report);
  EXPECT_EQ(report_call_count, 0);

  EXPECT_TRUE(mock_timer_ != nullptr);
  EXPECT_TRUE(mock_timer_->cb_);
  mock_timer_->cb_();
  EXPECT_EQ(report_call_count, 1);

  batch_->Flush();
  EXPECT_EQ(report_call_count, 1);
}

}  // namespace mixer_client
}  // namespace istio
