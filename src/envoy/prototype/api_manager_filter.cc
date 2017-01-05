#include "precompiled/precompiled.h"

#include "api_manager_env.h"
#include "common/common/logger.h"
#include "common/grpc/common.h"
#include "common/http/filter/ratelimit.h"
#include "common/http/headers.h"
#include "common/http/utility.h"
#include "contrib/endpoints/include/api_manager/api_manager.h"
#include "contrib/endpoints/src/grpc/transcoding/transcoder.h"
#include "envoy/server/instance.h"
#include "server/config/network/http_connection_manager.h"

namespace Http {
namespace ApiManager {

std::string ReadFile(const std::string& file_name) {
  std::ifstream t(file_name);
  std::string content((std::istreambuf_iterator<char>(t)),
                      std::istreambuf_iterator<char>());
  return content;
}

class Config : public Logger::Loggable<Logger::Id::http> {
 private:
  google::api_manager::ApiManagerFactory api_manager_factory_;
  std::shared_ptr<google::api_manager::ApiManager> api_manager_;
  Upstream::ClusterManager& cm_;

 public:
  Config(const Json::Object& config, Server::Instance& server)
      : cm_(server.clusterManager()) {
    std::string service_config_content;
    if (config.hasObject("service_config")) {
      const std::string service_config = config.getString("service_config");
      service_config_content = ReadFile(service_config);
    } else {
      log().error(
          "Service_config is required but not specified in the config: {}",
          __func__);
    }

    std::string server_config_content;
    if (config.hasObject("server_config")) {
      const std::string server_config = config.getString("server_config");
      server_config_content = ReadFile(server_config);
    }
    std::unique_ptr<google::api_manager::ApiManagerEnvInterface> env(
        new Env(server));

    api_manager_ = api_manager_factory_.GetOrCreateApiManager(
        std::move(env), service_config_content, server_config_content);

    api_manager_->Init();
    log().debug("Called ApiManager::Config constructor: {}", __func__);
  }

  std::shared_ptr<google::api_manager::ApiManager>& api_manager() {
    return api_manager_;
  }
};

typedef std::shared_ptr<Config> ConfigPtr;

class Request : public google::api_manager::Request {
 private:
  HeaderMap& header_map_;
  bool query_parsed_;
  std::map<std::string, std::string> query_params_;

 public:
  Request(HeaderMap& header_map)
      : header_map_(header_map), query_parsed_(false) {}
  virtual std::string GetRequestHTTPMethod() override {
    return header_map_.Method()->value().c_str();
  }
  virtual std::string GetRequestPath() override {
    return header_map_.Path()->value().c_str();
  }
  virtual std::string GetUnparsedRequestPath() override {
    return header_map_.Path()->value().c_str();
  }
  virtual std::string GetClientIP() override { return ""; }
  virtual bool FindQuery(const std::string& name, std::string* query) override {
    if (!query_parsed_) {
      auto header = header_map_.Path();
      if (header != nullptr) {
        std::string path = header->value().c_str();
        Utility::parseQueryString(path).swap(query_params_);
      }
      query_parsed_ = true;
    }
    auto entry = query_params_.find(name);
    if (entry == query_params_.end()) {
      return false;
    }
    *query = entry->second;
    return true;
  }

  virtual bool FindHeader(const std::string& name,
                          std::string* header) override {
    LowerCaseString key(name);
    const HeaderEntry* entry = header_map_.get(key);
    if (entry == nullptr) {
      return false;
    }
    *header = entry->value().c_str();
    return true;
  }

  virtual google::api_manager::protocol::Protocol GetRequestProtocol()
      override {
    return google::api_manager::protocol::Protocol::HTTP;
  }
  virtual google::api_manager::utils::Status AddHeaderToBackend(
      const std::string& key, const std::string& value) override {
    return google::api_manager::utils::Status::OK;
  }
  virtual void SetAuthToken(const std::string& auth_token) override {}

  virtual int64_t GetGrpcRequestBytes() { return 0; }
  virtual int64_t GetGrpcResponseBytes() { return 0; }
  virtual int64_t GetGrpcRequestMessageCounts() { return 0; }
  virtual int64_t GetGrpcResponseMessageCounts() { return 0; }
  virtual std::string GetQueryParameters() { return ""; }
};

class Response : public google::api_manager::Response {
  google::api_manager::utils::Status GetResponseStatus() {
    return google::api_manager::utils::Status::OK;
  }

  std::size_t GetRequestSize() { return 0; }

  std::size_t GetResponseSize() { return 0; }

  google::api_manager::utils::Status GetLatencyInfo(
      google::api_manager::service_control::LatencyInfo* info) {
    return google::api_manager::utils::Status::OK;
  }
};

const Http::HeaderMapImpl BadRequest{{Http::Headers::get().Status, "400"}};

class Instance : public Http::StreamFilter,
                 public Logger::Loggable<Logger::Id::http> {
 private:
  std::shared_ptr<google::api_manager::ApiManager> api_manager_;
  std::unique_ptr<google::api_manager::RequestHandlerInterface>
      request_handler_;

  enum State { NotStarted, Calling, Complete, Responded };
  State state_;

  StreamDecoderFilterCallbacks* decoder_callbacks_;
  StreamEncoderFilterCallbacks* encoder_callbacks_;

  bool initiating_call_;

 public:
  Instance(ConfigPtr config)
      : api_manager_(config->api_manager()),
        state_(NotStarted),
        initiating_call_(false) {
    log().debug("Called ApiManager::Instance : {}", __func__);
  }

  FilterHeadersStatus decodeHeaders(HeaderMap& headers,
                                    bool end_stream) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    std::unique_ptr<google::api_manager::Request> request(new Request(headers));
    request_handler_ = api_manager_->CreateRequestHandler(std::move(request));
    state_ = Calling;
    initiating_call_ = true;
    request_handler_->Check([this](google::api_manager::utils::Status status) {
      completeCheck(status);
    });
    initiating_call_ = false;

    if (state_ == Complete) {
      return FilterHeadersStatus::Continue;
    }
    log().debug("Called ApiManager::Instance : {} Stop", __func__);
    return FilterHeadersStatus::StopIteration;
  }

  FilterDataStatus decodeData(Buffer::Instance& data,
                              bool end_stream) override {
    log().debug("Called ApiManager::Instance : {} ({}, {})", __func__,
                data.length(), end_stream);
    if (state_ == Calling) {
      return FilterDataStatus::StopIterationAndBuffer;
    }
    return FilterDataStatus::Continue;
  }

  FilterTrailersStatus decodeTrailers(HeaderMap& trailers) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    if (state_ == Calling) {
      return FilterTrailersStatus::StopIteration;
    }
    return FilterTrailersStatus::Continue;
  }
  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    decoder_callbacks_ = &callbacks;
    decoder_callbacks_->addResetStreamCallback(
        [this]() { state_ = Responded; });
  }
  void completeCheck(const google::api_manager::utils::Status& status) {
    log().debug("Called ApiManager::Instance : check complete {}",
                status.ToJson());
    if (!status.ok() && state_ != Responded) {
      state_ = Responded;
      Utility::sendLocalReply(*decoder_callbacks_, Code(status.HttpCode()),
                              status.ToJson());
      return;
    }
    state_ = Complete;
    if (!initiating_call_) {
      decoder_callbacks_->continueDecoding();
    }
  }

  virtual FilterHeadersStatus encodeHeaders(HeaderMap& headers,
                                            bool end_stream) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    return FilterHeadersStatus::Continue;
  }
  virtual FilterDataStatus encodeData(Buffer::Instance& data,
                                      bool end_stream) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    return FilterDataStatus::Continue;
  }
  virtual FilterTrailersStatus encodeTrailers(HeaderMap& trailers) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    return FilterTrailersStatus::Continue;
  }
  virtual void setEncoderFilterCallbacks(
      StreamEncoderFilterCallbacks& callbacks) override {
    log().debug("Called ApiManager::Instance : {}", __func__);
    encoder_callbacks_ = &callbacks;
  }

  // note: cannot extend ~ActiveStream for access log, placing it here
  ~Instance() {
    log().debug("Called ApiManager::Instance : {}", __func__);
    std::unique_ptr<google::api_manager::Response> response(new Response());
    request_handler_->Report(std::move(response),
                             [this]() { log().debug("Report returns"); });
  }
};
}
}

namespace Server {
namespace Configuration {

class ApiManagerConfig : public HttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb tryCreateFilterFactory(
      HttpFilterType type, const std::string& name, const Json::Object& config,
      const std::string&, Server::Instance& server) override {
    if (type != HttpFilterType::Both || name != "esp") {
      return nullptr;
    }

    Http::ApiManager::ConfigPtr api_manager_config(
        new Http::ApiManager::Config(config, server));
    return [api_manager_config](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      auto instance = new Http::ApiManager::Instance(api_manager_config);
      callbacks.addStreamFilter(Http::StreamFilterPtr{instance});
    };
  }
};

static RegisterHttpFilterConfigFactory<ApiManagerConfig> register_;
}
}
