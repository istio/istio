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
#include "contrib/endpoints/src/api_manager/api_manager_impl.h"
#include "contrib/endpoints/src/api_manager/mock_api_manager_environment.h"
#include "gtest/gtest.h"

using ::testing::_;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::Return;

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {

namespace {

const char kServerConfigWithServiceNameConfigId[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "service_name": "bookstore.test.appspot.com",
  "config_id": "2017-05-01r0"
}
)";

const char kServiceConfig1[] = R"(
{
  "name": "bookstore.test.appspot.com",
  "title": "Bookstore",
  "http": {
    "rules": [
      {
        "selector": "EchoGetMessage",
        "get": "/echo"
      }
    ]
  },
  "usage": {
    "rules": [
      {
        "selector": "EchoGetMessage",
        "allowUnregisteredCalls": true
      }
    ]
  },
  "control": {
    "environment": "servicecontrol.googleapis.com"
  },
  "id": "2017-05-01r0"
}
)";

const char kReportResponseSucceeded[] = R"(
service_config_id: "2017-02-08r9"

)";

const char kServiceForStatistics[] =
    "name: \"service-name\"\n"
    "control: {\n"
    "  environment: \"http://127.0.0.1:8081\"\n"
    "}\n";

class RequestMock : public Request {
 public:
  RequestMock(std::unordered_map<std::string, std::string> data)
      : data_(data) {}
  virtual ~RequestMock() {}

  std::string GetRequestHTTPMethod() { return data_["method"]; }
  std::string GetQueryParameters() { return data_["query"]; }
  std::string GetRequestPath() { return data_["path"]; }
  std::string GetUnparsedRequestPath() { return data_["path"]; }
  std::string GetClientIP() { return data_["ip"]; }
  std::string GetClientHost() { return data_["host"]; }
  int64_t GetGrpcRequestBytes() { return 0; }
  int64_t GetGrpcResponseBytes() { return 0; }
  int64_t GetGrpcRequestMessageCounts() { return 0; }
  int64_t GetGrpcResponseMessageCounts() { return 0; }

  bool FindQuery(const std::string &name, std::string *query) {
    if (data_.find("query." + name) == data_.end()) {
      return false;
    }
    *query = data_["query." + name];
    return true;
  }

  bool FindHeader(const std::string &name, std::string *header) {
    if (data_.find("header." + name) == data_.end()) {
      return false;
    }
    *header = data_["header." + name];
    return true;
  }

  ::google::api_manager::protocol::Protocol GetFrontendProtocol() {
    return ::google::api_manager::protocol::Protocol::HTTP;
  }

  ::google::api_manager::protocol::Protocol GetBackendProtocol() {
    return ::google::api_manager::protocol::Protocol::HTTPS;
  }

  void SetAuthToken(const std::string &auth_token) {}

  utils::Status AddHeaderToBackend(const std::string &key,
                                   const std::string &value) {
    return utils::Status::OK;
  }

 private:
  std::unordered_map<std::string, std::string> data_;
};

class ResponseMock : public Response {
 public:
  virtual ~ResponseMock() {}
  utils::Status GetResponseStatus() { return utils::Status::OK; }
  std::size_t GetRequestSize() { return 0; }
  std::size_t GetResponseSize() { return 0; }
  utils::Status GetLatencyInfo(service_control::LatencyInfo *info) {
    return utils::Status::OK;
  }
};

class RequestHandlerTest : public ::testing::Test {
 protected:
  RequestHandlerTest() : callback_run_count_(0), raw_env_(nullptr) {}

  void SetUp() {
    callback_run_count_ = 0;

    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>());
    raw_env_ = env_.get();

    api_manager_ = std::make_shared<ApiManagerImpl>(
        std::move(env_), "", kServerConfigWithServiceNameConfigId);
    EXPECT_TRUE(api_manager_);

    request_handler_ = api_manager_->CreateRequestHandler(
        std::unique_ptr<Request>(new RequestMock({{"method", "GET"},
                                                  {"ip", "127.0.0.1"},
                                                  {"host", "localhost"},
                                                  {"path", "/echo"}})));
  }

 protected:
  int callback_run_count_;
  std::shared_ptr<ApiManagerImpl> api_manager_;
  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockApiManagerEnvironment *raw_env_;
  std::unique_ptr<RequestHandlerInterface> request_handler_;

 private:
  ApiManagerFactory factory_;
};

TEST_F(RequestHandlerTest, PendingCheckApiManagerInitSucceeded) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, std::move(kServiceConfig1));
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  req->url());
        req->OnComplete(Status::OK, {}, "{}");

      }));

  EXPECT_EQ("UNAVAILABLE: Not initialized yet",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager_->service_name());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  // api-key is not required and api-key is not provided, so
  // not need to make remote call for Check().
  request_handler_->Check([this](utils::Status status) {
    callback_run_count_++;
    EXPECT_OK(status);
  });

  EXPECT_EQ(0, callback_run_count_);

  api_manager_->Init();

  EXPECT_EQ(1, callback_run_count_);

  EXPECT_EQ("OK", api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_TRUE(api_manager_->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager_->service("2017-05-01r0").id());
}

TEST_F(RequestHandlerTest, PendingCheckApiManagerInitSucceededBackendFailed) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, std::move(kServiceConfig1));
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  req->url());
        req->OnComplete(Status::OK, {}, "{}");

      }));

  EXPECT_EQ("UNAVAILABLE: Not initialized yet",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager_->service_name());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  std::unique_ptr<RequestHandlerInterface> request_handler =
      api_manager_->CreateRequestHandler(
          std::unique_ptr<Request>(new RequestMock({{"method", "GET"},
                                                    {"ip", "127.0.0.1"},
                                                    {"host", "localhost"},
                                                    {"path", "/"}})));

  request_handler->Check([this](utils::Status status) {
    callback_run_count_++;
    // Backend error
    EXPECT_EQ("NOT_FOUND: Method does not exist.", status.ToString());
  });

  EXPECT_EQ(0, callback_run_count_);

  // Initialize the ApiManager then run pending callback.
  api_manager_->Init();

  EXPECT_EQ(1, callback_run_count_);

  // Successfully initialized by ConfigManager
  EXPECT_EQ("OK", api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_TRUE(api_manager_->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager_->service("2017-05-01r0").id());
}

TEST_F(RequestHandlerTest, PendCheckReportApiManagerInitSucceeded) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, std::move(kServiceConfig1));
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  req->url());
        req->OnComplete(Status::OK, {}, "{}");
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicecontrol.googleapis.com/v1/services/"
            "bookstore.test.appspot.com:report",
            req->url());
        req->OnComplete(Status::OK, {}, kReportResponseSucceeded);
      }));

  EXPECT_EQ("UNAVAILABLE: Not initialized yet",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager_->service_name());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  // Pending request callback will be registered here and
  // will be executed after api_manager->Init();
  request_handler_->Check([this](utils::Status status) {
    callback_run_count_++;
    // Initialization was succeeded. Backend error
    EXPECT_EQ(1, callback_run_count_);
    EXPECT_OK(status);
  });

  std::unique_ptr<Response> response(new ResponseMock());

  // Pending request callback will be registered here and
  // will be executed after api_manager->Init();
  request_handler_->Report(std::move(response), [this]() {
    callback_run_count_++;
    EXPECT_EQ(2, callback_run_count_);
  });

  EXPECT_EQ(0, callback_run_count_);

  // Initialize the ApiManager then run pending callback.
  api_manager_->Init();

  // Check two pending callbacks were executed
  EXPECT_EQ(2, callback_run_count_);

  // Successfully initialized by ConfigManager
  EXPECT_EQ("OK", api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_TRUE(api_manager_->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager_->service("2017-05-01r0").id());
}

TEST_F(RequestHandlerTest, PendigCheckApiManagerInitSucceededReport) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, std::move(kServiceConfig1));
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  req->url());
        req->OnComplete(Status::OK, {}, "{}");
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicecontrol.googleapis.com/v1/services/"
            "bookstore.test.appspot.com:report",
            req->url());
        req->OnComplete(Status::OK, {}, kReportResponseSucceeded);
      }));

  EXPECT_EQ("UNAVAILABLE: Not initialized yet",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager_->service_name());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  std::unique_ptr<Response> response(new ResponseMock());

  request_handler_->Check([this](utils::Status status) {
    callback_run_count_++;
    EXPECT_EQ(1, callback_run_count_);
    EXPECT_OK(status);
  });

  EXPECT_EQ(0, callback_run_count_);

  api_manager_->Init();

  EXPECT_EQ(1, callback_run_count_);

  EXPECT_EQ("OK", api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_TRUE(api_manager_->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager_->service("2017-05-01r0").id());

  // Call Report synchronous
  request_handler_->Report(std::move(response), [this]() {
    callback_run_count_++;
    EXPECT_EQ(2, callback_run_count_);
  });

  // Report callback was executed before this line
  EXPECT_EQ(2, callback_run_count_);
}

TEST_F(RequestHandlerTest, PendingReportApiManagerInitSucceeded) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, std::move(kServiceConfig1));
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicecontrol.googleapis.com/v1/services/"
            "bookstore.test.appspot.com:report",
            req->url());
        req->OnComplete(Status::OK, {}, kReportResponseSucceeded);
      }));

  EXPECT_EQ("UNAVAILABLE: Not initialized yet",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager_->service_name());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  std::unique_ptr<Response> response(new ResponseMock());

  request_handler_->Report(std::move(response),
                           [this]() { callback_run_count_++; });

  EXPECT_EQ(0, callback_run_count_);

  api_manager_->Init();

  EXPECT_EQ(1, callback_run_count_);

  EXPECT_EQ("OK", api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_TRUE(api_manager_->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager_->service("2017-05-01r0").id());
}

TEST_F(RequestHandlerTest, PendingCheckApiManagerInitializationFailed) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        EXPECT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r0",
            req->url());

        req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {}, "");
      }));

  EXPECT_EQ("UNAVAILABLE: Not initialized yet",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager_->service_name());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  request_handler_->Check([this](utils::Status status) {
    callback_run_count_++;
    EXPECT_OK(status);
  });
  EXPECT_EQ(0, callback_run_count_);

  api_manager_->Init();

  EXPECT_EQ(1, callback_run_count_);

  // Unable to download service_config. Failed to load.
  EXPECT_EQ("ABORTED: Failed to download the service config",
            api_manager_->ConfigLoadingStatus().ToString());
  EXPECT_FALSE(api_manager_->Enabled());
  EXPECT_EQ("", api_manager_->service("2017-05-01r0").id());

  // ApiManager initialization was failed.
  // Report callback will be called right away.
  std::unique_ptr<Response> response(new ResponseMock());
  request_handler_->Report(std::move(response), [this]() {
    callback_run_count_++;
    EXPECT_EQ(2, callback_run_count_);
  });

  EXPECT_EQ(2, callback_run_count_);
}

}  // namespace

}  // namespace api_manager
}  // namespace google
