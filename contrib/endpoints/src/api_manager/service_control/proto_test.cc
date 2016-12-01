// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/service_control/proto.h"
#include "gtest/gtest.h"

#include <assert.h>
#include <fstream>
#include <string>

#include "google/protobuf/struct.pb.h"
#include "google/protobuf/text_format.h"

#include "include/api_manager/version.h"

namespace gasv1 = ::google::api::servicecontrol::v1;
using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;
using ::google::protobuf::Timestamp;

namespace google {
namespace api_manager {
namespace service_control {

namespace {

const char kTestdata[] = "src/api_manager/service_control/testdata/";

std::string ReadTestBaseline(const char* input_file_name) {
  std::string file_name = std::string(kTestdata) + input_file_name;

  std::string contents;
  std::ifstream input_file;
  input_file.open(file_name, std::ifstream::in | std::ifstream::binary);
  EXPECT_TRUE(input_file.is_open()) << file_name;
  input_file.seekg(0, std::ios::end);
  contents.reserve(input_file.tellg());
  input_file.seekg(0, std::ios::beg);
  contents.assign((std::istreambuf_iterator<char>(input_file)),
                  (std::istreambuf_iterator<char>()));

  // Replace instances of {{service_agent_version}} with the expected service
  // agent version.
  std::string placeholder = "{{service_agent_version}}";
  std::string value = API_MANAGER_VERSION_STRING;
  size_t current = 0;
  while ((current = contents.find(placeholder, current)) != std::string::npos) {
    contents.replace(current, placeholder.length(), value);
    current += value.length();
  }
  return contents;
}

void FillOperationInfo(OperationInfo* op) {
  op->operation_id = "operation_id";
  op->operation_name = "operation_name";
  op->api_key = "api_key_x";
  op->producer_project_id = "project_id";
}

void FillCheckRequestInfo(CheckRequestInfo* request) {
  request->client_ip = "1.2.3.4";
  request->referer = "referer";
}

void FillReportRequestInfo(ReportRequestInfo* request) {
  request->referer = "referer";
  request->response_code = 200;
  request->location = "us-central";
  request->api_name = "api-name";
  request->api_version = "api-version";
  request->api_method = "api-method";
  request->request_size = 100;
  request->response_size = 1024 * 1024;
  request->log_message = "test-method is called";
  request->latency.request_time_ms = 123;
  request->latency.backend_time_ms = 101;
  request->latency.overhead_time_ms = 22;
  request->protocol = protocol::HTTP;
  request->compute_platform = compute_platform::GKE;
  request->auth_issuer = "auth-issuer";
  request->auth_audience = "auth-audience";

  request->request_bytes = 100;
  request->response_bytes = 1024 * 1024;
}

void SetFixTimeStamps(gasv1::Operation* op) {
  Timestamp fix_time;
  fix_time.set_seconds(100000);
  fix_time.set_nanos(100000);
  *op->mutable_start_time() = fix_time;
  *op->mutable_end_time() = fix_time;
  if (op->log_entries().size() > 0) {
    *op->mutable_log_entries(0)->mutable_timestamp() = fix_time;
    op->mutable_log_entries(0)
        ->mutable_struct_payload()
        ->mutable_fields()
        ->erase("timestamp");
  }
}

std::string CheckRequestToString(gasv1::CheckRequest* request) {
  gasv1::Operation* op = request->mutable_operation();
  SetFixTimeStamps(op);

  std::string text;
  google::protobuf::TextFormat::PrintToString(*request, &text);
  return text;
}

std::string ReportRequestToString(gasv1::ReportRequest* request) {
  gasv1::Operation* op = request->mutable_operations(0);
  SetFixTimeStamps(op);

  std::string text;
  google::protobuf::TextFormat::PrintToString(*request, &text);
  return text;
}

class ProtoTest : public ::testing::Test {
 protected:
  ProtoTest() : scp_({"local_test_log"}, "test_service", "2016-09-19r0") {}

  Proto scp_;
};

TEST(Proto, TestProtobufStruct) {
  // Verify if ::google::protobuf::Struct works.
  // If the main binary code is compiled with CXXFLAGS=-std=c++11,
  // and protobuf library is not, ::google::protobuf::Struct will crash.
  ::google::protobuf::Struct st;
  auto* fields = st.mutable_fields();
  (*fields)["test"].set_string_value("value");
  ASSERT_FALSE(fields->empty());
}

TEST_F(ProtoTest, FillGoodCheckRequestTest) {
  CheckRequestInfo info;
  FillOperationInfo(&info);
  FillCheckRequestInfo(&info);

  gasv1::CheckRequest request;
  ASSERT_TRUE(scp_.FillCheckRequest(info, &request).ok());

  std::string text = CheckRequestToString(&request);
  std::string expected_text = ReadTestBaseline("check_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, FillNoApiKeyCheckRequestTest) {
  CheckRequestInfo info;
  info.operation_id = "operation_id";
  info.operation_name = "operation_name";
  info.producer_project_id = "project_id";

  gasv1::CheckRequest request;
  ASSERT_TRUE(scp_.FillCheckRequest(info, &request).ok());

  std::string text = CheckRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("check_request_no_api_key.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, CheckRequestMissingOperationNameTest) {
  CheckRequestInfo info;
  info.operation_id = "operation_id";

  gasv1::CheckRequest request;
  ASSERT_EQ(Code::INVALID_ARGUMENT,
            scp_.FillCheckRequest(info, &request).code());
}

TEST_F(ProtoTest, CheckRequestMissingOperationIdTest) {
  CheckRequestInfo info;
  info.operation_name = "operation_name";

  gasv1::CheckRequest request;
  ASSERT_EQ(Code::INVALID_ARGUMENT,
            scp_.FillCheckRequest(info, &request).code());
}

TEST_F(ProtoTest, FillGoodReportRequestTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, FillStartReportRequestTest) {
  ReportRequestInfo info;
  info.is_first_report = true;
  info.is_final_report = false;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("first_report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, FillIntermediateReportRequestTest) {
  ReportRequestInfo info;
  info.is_first_report = false;
  info.is_final_report = false;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("intermediate_report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, FillFinalReportRequestTest) {
  ReportRequestInfo info;
  info.is_first_report = false;
  info.is_final_report = true;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("final_report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, FillReportRequestFailedTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  // Remove api_key to test not api_key case for
  // producer_project_id and credential_id.
  info.api_key = "";
  FillReportRequestInfo(&info);

  // Use 401 as a failed response code.
  info.response_code = 401;

  // Use the corresponding status for that response code.
  info.status = Status(info.response_code, "", Status::APPLICATION);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("report_request_failed.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, FillReportRequestEmptyOptionalTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("report_request_empty_optional.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(ProtoTest, CredentailIdApiKeyTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  ASSERT_EQ(request.operations(0).labels().at("/credential_id"),
            "apikey:api_key_x");
}

TEST_F(ProtoTest, CredentailIdIssuerOnlyTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  info.api_key = "";
  info.auth_issuer = "auth-issuer";

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  ASSERT_EQ(request.operations(0).labels().at("/credential_id"),
            "jwtauth:issuer=YXV0aC1pc3N1ZXI");
}

TEST_F(ProtoTest, CredentailIdIssuerAudienceTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  info.api_key = "";
  info.auth_issuer = "auth-issuer";
  info.auth_audience = "auth-audience";

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  ASSERT_EQ(request.operations(0).labels().at("/credential_id"),
            "jwtauth:issuer=YXV0aC1pc3N1ZXI&audience=YXV0aC1hdWRpZW5jZQ");
}

}  // namespace

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
