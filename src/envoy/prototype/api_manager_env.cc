#include "api_manager_env.h"

#include "common/http/headers.h"
#include "common/http/message_impl.h"
#include "envoy/event/timer.h"

namespace Http {
namespace ApiManager {

void Http::ApiManager::Env::Log(LogLevel level, const char *message) {
  switch (level) {
    case LogLevel::DEBUG:
      log().debug("{}", message);
      break;
    case LogLevel::INFO:
      log().debug("{}", message);
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
  virtual std::string bodyAsString() override { return ""; }
};

class RequestCallbacks : public AsyncClient::Callbacks {
 private:
  std::unique_ptr<google::api_manager::HTTPRequest> request_;
  std::unique_ptr<AsyncClient::Request> sent_request_;

 public:
  RequestCallbacks(std::unique_ptr<google::api_manager::HTTPRequest> &&request)
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
    google::api_manager::utils::Status status =
        google::api_manager::utils::Status::OK;
    std::map<std::string, std::string> headers;
    request_->OnComplete(status, std::move(headers), "");
    delete this;
  }
};

void Env::RunHTTPRequest(
    std::unique_ptr<google::api_manager::HTTPRequest> request) {
  auto &client = cm_.httpAsyncClientForCluster("api_manager");

  MessagePtr message{new HTTPRequest(request.get())};
  RequestCallbacks *callbacks = new RequestCallbacks(std::move(request));
  client.send(
      std::move(message), *callbacks,
      Optional<std::chrono::milliseconds>(std::chrono::milliseconds(10000)));
}

void Env::RunGRPCRequest(
    std::unique_ptr<google::api_manager::GRPCRequest> request) {
  // TODO: send grpc request.
}
}
}
