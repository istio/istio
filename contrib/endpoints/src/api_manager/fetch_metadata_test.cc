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
#include "src/api_manager/fetch_metadata.h"

#include "src/api_manager/context/service_context.h"
#include "src/api_manager/mock_api_manager_environment.h"
#include "src/api_manager/mock_request.h"

using ::testing::_;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::Return;

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {

namespace {

const char kServiceConfig[] = R"(
{
  "name": "endpoints-test.cloudendpointsapis.com",
  "control": {
     "environment": "http://127.0.0.1:808"
  }
})";

const char kEmptyBody[] = R"({})";
const char kMetadataServer[] = "http://127.0.0.1:8090";

class FetchMetadataTest : public ::testing::Test {
 public:
  void SetUp() {
    std::unique_ptr<MockApiManagerEnvironment> env(
        new ::testing::NiceMock<MockApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env.get();

    std::unique_ptr<Config> config =
        Config::Create(raw_env_, kServiceConfig, "");
    ASSERT_NE(config.get(), nullptr);

    service_context_ = std::make_shared<context::ServiceContext>(
        std::move(env), std::move(config));
    ASSERT_NE(service_context_.get(), nullptr);

    service_context_->SetMetadataServer(kMetadataServer);

    std::unique_ptr<MockRequest> request(
        new ::testing::NiceMock<MockRequest>());

    context_ = std::make_shared<context::RequestContext>(service_context_,
                                                         std::move(request));
  }

  MockApiManagerEnvironment *raw_env_;
  std::shared_ptr<context::ServiceContext> service_context_;
  std::shared_ptr<context::RequestContext> context_;
};

TEST_F(FetchMetadataTest, FetchGceMetadataWithStatusOK) {
  // FetchGceMetadata responses with headers and status OK.
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([](HTTPRequest *req) {
        std::map<std::string, std::string> empty;
        std::string body(kEmptyBody);
        req->OnComplete(Status::OK, std::move(empty), std::move(body));
      }));

  FetchGceMetadata(context_, [](Status status) { ASSERT_TRUE(status.ok()); });
}

TEST_F(FetchMetadataTest, FetchGceMetadataWithStatusINTERNAL) {
  // FetchGceMetadata responses with headers and status UNAVAILABLE.
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([](HTTPRequest *req) {
        std::map<std::string, std::string> empty;
        std::string body(kEmptyBody);
        req->OnComplete(Status(Code::UNKNOWN, ""), std::move(empty),
                        std::move(body));
      }));

  FetchGceMetadata(context_, [](Status status) {
    ASSERT_EQ(Code::INTERNAL, status.code());
  });
}

}  // namespace

}  // namespace api_manager
}  // namespace google
