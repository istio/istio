#include "api_manager_env.h"

#include "common/http/headers.h"
#include "common/http/message_impl.h"
#include "envoy/event/timer.h"
#include "google/protobuf/stubs/status.h"
#include "source/common/grpc/common.h"

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace Http {
namespace ApiManager {

void Http::ApiManager::Env::Log(LogLevel level, const char *message) {
  switch (level) {
    case LogLevel::DEBUG:
      log().debug("{}", message);
      break;
    case LogLevel::INFO:
      log().info("{}", message);
      break;
    case LogLevel::WARNING:
      log().warn("{}", message);
      break;
    case LogLevel::ERROR:
      log().error("{}", message);
      break;
  }
}

class PeriodicTimer : public google::api_manager::PeriodicTimer,
                      public Logger::Loggable<Logger::Id::http> {
 private:
  Server::Instance &server_;
  Event::TimerPtr timer_;

 public:
  PeriodicTimer(Server::Instance &server) : server_(server) {}
  ~PeriodicTimer() { Stop(); }
  void Stop() {
    if (timer_) {
      timer_->disableTimer();
      timer_ = nullptr;
    }
  }
  void Schedule(std::chrono::milliseconds interval,
                std::function<void()> continuation) {
    Stop();
    timer_ = server_.dispatcher().createTimer([this, continuation, interval]() {
      continuation();
      Schedule(interval, continuation);
    });
    timer_->enableTimer(interval);
  }
};

std::unique_ptr<google::api_manager::PeriodicTimer> Env::StartPeriodicTimer(
    std::chrono::milliseconds interval, std::function<void()> continuation) {
  log().debug("start periodic timer");
  auto single = new PeriodicTimer(server);
  single->Schedule(interval, continuation);
  std::unique_ptr<google::api_manager::PeriodicTimer> timer(single);
  return timer;
}

static const LowerCaseString kApiManagerUrl("x-api-manager-url");
static const LowerCaseString kGrpcTEKey("te");
static const std::string kGrpcTEValue("trailers");

class HTTPRequest : public Http::Message {
 private:
  HeaderMapImpl header_map_;
  Buffer::OwnedImpl body_;

 public:
  HTTPRequest(google::api_manager::HTTPRequest *request)
      : body_(request->body()) {
    header_map_.addStaticKey(Headers::get().Method, request->method());

    size_t path_pos = request->url().find('/', 8);
    if (path_pos == std::string::npos) {
      header_map_.addStaticKey(Headers::get().Path, "/");
    } else {
      header_map_.addStaticKey(Headers::get().Path,
                               request->url().substr(path_pos));
    }

    header_map_.addStaticKey(Headers::get().Scheme, "http");
    header_map_.addStaticKey(Headers::get().Host, "localhost");
    header_map_.addStaticKey(Headers::get().ContentLength, body_.length());
    header_map_.addStaticKey(kApiManagerUrl, request->url());
    for (const auto header : request->request_headers()) {
      LowerCaseString lower_key(header.first);
      HeaderString key, value;
      key.setCopy(lower_key.get().data(), lower_key.get().size());
      value.setCopy(header.second.data(), header.second.size());
      header_map_.addViaMove(std::move(key), std::move(value));
    }
  }
  virtual HeaderMap &headers() override { return header_map_; }
  virtual Buffer::Instance *body() override { return &body_; }
  virtual void body(Buffer::InstancePtr &&body) override {}
  virtual HeaderMap *trailers() override { return nullptr; }
  virtual void trailers(HeaderMapPtr &&trailers) override {}
  virtual std::string bodyAsString() const override { return ""; }
};

class HTTPRequestCallbacks : public AsyncClient::Callbacks {
 private:
  std::unique_ptr<google::api_manager::HTTPRequest> request_;
  std::unique_ptr<AsyncClient::Request> sent_request_;

 public:
  HTTPRequestCallbacks(
      std::unique_ptr<google::api_manager::HTTPRequest> &&request)
      : request_(std::move(request)) {}
  virtual void onSuccess(MessagePtr &&response) override {
    google::api_manager::utils::Status status(
        std::stoi(response->headers().Status()->value().c_str()), "");
    std::map<std::string, std::string> headers;
    response->headers().iterate(
        [&](const HeaderEntry &header, void *) -> void {
          // TODO: fix it
          // headers.emplace(header.key().c_str(), header.value().c_str());
        },
        nullptr);
    request_->OnComplete(status, std::move(headers), response->bodyAsString());
    delete this;
  }
  virtual void onFailure(AsyncClient::FailureReason reason) override {
    google::api_manager::utils::Status status(-1,
                                              "Cannot connect to HTTP server.");
    std::map<std::string, std::string> headers;
    request_->OnComplete(status, std::move(headers), "");
    delete this;
  }
};

namespace {
// Copy the code here from envoy/grpc/common.cc
Buffer::InstancePtr SerializeGrpcBody(const std::string &body_str) {
  // http://www.grpc.io/docs/guides/wire.html
  Buffer::InstancePtr body(new Buffer::OwnedImpl());
  uint8_t compressed = 0;
  body->add(&compressed, sizeof(compressed));
  uint32_t size = htonl(body_str.size());
  body->add(&size, sizeof(size));
  body->add(body_str);
  return body;
}
Http::MessagePtr PrepareGrpcHeaders(const std::string &upstream_cluster,
                                    const std::string &service_full_name,
                                    const std::string &method_name) {
  Http::MessagePtr message(new Http::RequestMessageImpl());
  message->headers().insertMethod().value(
      Http::Headers::get().MethodValues.Post);
  message->headers().insertPath().value(
      fmt::format("/{}/{}", service_full_name, method_name));
  message->headers().insertHost().value(upstream_cluster);
  message->headers().insertContentType().value(Grpc::Common::GRPC_CONTENT_TYPE);
  message->headers().addStatic(kGrpcTEKey, kGrpcTEValue);
  return message;
}
}  // annoymous namespace

class GrpcRequestCallbacks : public AsyncClient::Callbacks {
 private:
  Env *env_;
  std::unique_ptr<google::api_manager::GRPCRequest> request_;

 public:
  GrpcRequestCallbacks(
      Env *env, std::unique_ptr<google::api_manager::GRPCRequest> &&request)
      : env_(env), request_(std::move(request)) {}
  virtual void onSuccess(MessagePtr &&response) override {
    google::api_manager::utils::Status status(
        std::stoi(response->headers().Status()->value().c_str()), "");
    Grpc::Common::validateResponse(*response);
    env_->LogInfo("pass validate");
    // remove 5 bytes of grpc header
    response->body()->drain(5);
    request_->OnComplete(status, response->bodyAsString());
    delete this;
  }
  virtual void onFailure(AsyncClient::FailureReason reason) override {
    google::api_manager::utils::Status status(-1,
                                              "Cannot connect to gRPC server.");
    request_->OnComplete(status, "");
    delete this;
  }
};

void Env::RunHTTPRequest(
    std::unique_ptr<google::api_manager::HTTPRequest> request) {
  auto &client = cm_.httpAsyncClientForCluster("api_manager");

  MessagePtr message{new HTTPRequest(request.get())};
  HTTPRequestCallbacks *callbacks =
      new HTTPRequestCallbacks(std::move(request));
  client.send(
      std::move(message), *callbacks,
      Optional<std::chrono::milliseconds>(std::chrono::milliseconds(10000)));
}

void Env::RunGRPCRequest(
    std::unique_ptr<google::api_manager::GRPCRequest> request) {
  auto &client = cm_.httpAsyncClientForCluster(request->server());

  Http::MessagePtr message =
      PrepareGrpcHeaders("localhost", request->service(), request->method());
  message->body(SerializeGrpcBody(request->body()));
  auto callbacks = new GrpcRequestCallbacks(this, std::move(request));
  client.send(
      std::move(message), *callbacks,
      Optional<std::chrono::milliseconds>(std::chrono::milliseconds(10000)));
}
}
}
