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
#include "src/api_manager/service_control/aggregated.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "include/api_manager/utils/status.h"
#include "src/api_manager/mock_api_manager_environment.h"
#include "src/api_manager/service_control/proto.h"

using ::google::api::servicecontrol::v1::CheckRequest;
using ::google::api::servicecontrol::v1::CheckResponse;
using ::google::api::servicecontrol::v1::ReportRequest;
using ::google::api::servicecontrol::v1::ReportResponse;
using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;
using ::google::service_control_client::ServiceControlClient;
using ::google::service_control_client::TransportCheckFunc;
using ::google::service_control_client::TransportReportFunc;
using ::testing::Return;
using ::testing::Invoke;
using ::testing::_;

namespace google {
namespace api_manager {
namespace service_control {

namespace {
void FillOperationInfo(OperationInfo* op) {
  op->operation_id = "operation_id";
  op->operation_name = "operation_name";
  op->api_key = "api_key_x";
  op->producer_project_id = "project_id";
}

class MockServiceControClient : public ServiceControlClient {
 public:
  MOCK_METHOD3(Check, void(const CheckRequest&, CheckResponse*, DoneCallback));
  MOCK_METHOD2(Check, ::google::protobuf::util::Status(const CheckRequest&,
                                                       CheckResponse*));
  MOCK_METHOD4(Check, void(const CheckRequest&, CheckResponse*, DoneCallback,
                           TransportCheckFunc));
  MOCK_METHOD3(Report,
               void(const ReportRequest&, ReportResponse*, DoneCallback));
  MOCK_METHOD2(Report, ::google::protobuf::util::Status(const ReportRequest&,
                                                        ReportResponse*));
  MOCK_METHOD4(Report, void(const ReportRequest&, ReportResponse*, DoneCallback,
                            TransportReportFunc));
  MOCK_CONST_METHOD1(GetStatistics,
                     ::google::protobuf::util::Status(
                         ::google::service_control_client::Statistics*));
};
}  // namespace

class AggregatedTestWithMockedClient : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>);
    mock_client_ = new MockServiceControClient;
    sc_lib_.reset(
        new Aggregated({"local_test_log"}, env_.get(),
                       std::unique_ptr<ServiceControlClient>(mock_client_)));
    ASSERT_TRUE((bool)(sc_lib_));
  }

  void Check(const CheckRequest& req, CheckResponse* res,
             ServiceControlClient::DoneCallback on_done,
             TransportCheckFunc transport) {
    on_done(done_status_);
  }
  void Report(const ReportRequest& req, ReportResponse* res,
              ServiceControlClient::DoneCallback on_done) {
    on_done(done_status_);
  }

  ::google::protobuf::util::Status done_status_;
  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockServiceControClient* mock_client_;
  std::unique_ptr<Interface> sc_lib_;
};

TEST_F(AggregatedTestWithMockedClient, ReportTest) {
  EXPECT_CALL(*mock_client_, Report(_, _, _))
      .WillOnce(Invoke(this, &AggregatedTestWithMockedClient::Report));
  ReportRequestInfo info;
  FillOperationInfo(&info);
  // mock the client to return OK
  done_status_ = ::google::protobuf::util::Status::OK;
  ASSERT_TRUE(sc_lib_->Report(info).ok());
}

TEST_F(AggregatedTestWithMockedClient, FailedReportTest) {
  EXPECT_CALL(*mock_client_, Report(_, _, _))
      .WillOnce(Invoke(this, &AggregatedTestWithMockedClient::Report));
  ReportRequestInfo info;
  FillOperationInfo(&info);
  // mock the client to return failed status.
  done_status_ = ::google::protobuf::util::Status(
      Code::INTERNAL, "AggregatedTestWithMockedClient internal error");
  // Client layer failure is ignored.
  ASSERT_TRUE(sc_lib_->Report(info).ok());
}

TEST_F(AggregatedTestWithMockedClient, FailedCheckRequiredFieldTest) {
  CheckRequestInfo info;
  FillOperationInfo(&info);
  info.operation_name = nullptr;  // Missing operation_name
  sc_lib_->Check(info, nullptr,
                 [](Status status, const CheckResponseInfo& info) {
                   ASSERT_EQ(Code::INVALID_ARGUMENT, status.code());
                 });
}

TEST_F(AggregatedTestWithMockedClient, CheckTest) {
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke(this, &AggregatedTestWithMockedClient::Check));
  CheckRequestInfo info;
  FillOperationInfo(&info);
  // mock the client to return OK
  done_status_ = ::google::protobuf::util::Status::OK;
  sc_lib_->Check(info, nullptr,
                 [](Status status, const CheckResponseInfo& info) {
                   ASSERT_TRUE(status.ok());
                 });
}

TEST_F(AggregatedTestWithMockedClient, FailedCheckTest) {
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke(this, &AggregatedTestWithMockedClient::Check));
  CheckRequestInfo info;
  FillOperationInfo(&info);
  // mock the client to return OK
  done_status_ = ::google::protobuf::util::Status(
      Code::INTERNAL, "AggregatedTestWithMockedClient internal error");
  sc_lib_->Check(info, nullptr,
                 [](Status status, const CheckResponseInfo& info) {
                   ASSERT_EQ(status.code(), Code::INTERNAL);
                 });
}

class AggregatedTestWithRealClient : public ::testing::Test {
 public:
  void SetUp() {
    service_.set_name("test_service");
    service_.mutable_control()->set_environment(
        "servicecontrol.googleapis.com");
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>);
    sc_lib_.reset(Aggregated::Create(service_, nullptr, env_.get(), nullptr));
    ASSERT_TRUE((bool)(sc_lib_));
    // This is the call actually creating the client.
    sc_lib_->Init();
  }

  void DoRunHTTPRequest(HTTPRequest* request) {
    std::map<std::string, std::string> headers;
    std::string body;
    request->OnComplete(Status::OK, std::move(headers), std::move(body));
  }

  ::google::api::Service service_;
  std::unique_ptr<MockApiManagerEnvironment> env_;
  std::unique_ptr<Interface> sc_lib_;
};

TEST_F(AggregatedTestWithRealClient, CheckOKTest) {
  EXPECT_CALL(*env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke(this, &AggregatedTestWithRealClient::DoRunHTTPRequest));

  CheckRequestInfo info;
  FillOperationInfo(&info);
  sc_lib_->Check(info, nullptr,
                 [](Status status, const CheckResponseInfo& info) {
                   ASSERT_TRUE(status.ok());
                 });

  Statistics stat;
  Status stat_status = sc_lib_->GetStatistics(&stat);
  EXPECT_EQ(stat_status, Status::OK);
  EXPECT_EQ(stat.total_called_checks, 1);
  EXPECT_EQ(stat.send_checks_by_flush, 0);
  EXPECT_EQ(stat.send_checks_in_flight, 1);
  EXPECT_EQ(stat.send_report_operations, 0);
}

TEST(AggregatedServiceControlTest, Create) {
  // Verify that invalid service config yields nullptr.
  ::google::api::Service
      invalid_service;  // only contains name, not service control address.
  invalid_service.set_name("invalid-service");

  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>);
  std::unique_ptr<Interface> sc_lib(
      Aggregated::Create(invalid_service, nullptr, env.get(), nullptr));
  ASSERT_FALSE(sc_lib);
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
