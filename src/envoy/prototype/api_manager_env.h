#pragma once

#include "precompiled/precompiled.h"

#include "common/common/logger.h"
#include "contrib/endpoints/include/api_manager/env_interface.h"
#include "envoy/upstream/cluster_manager.h"
#include "server/server.h"

namespace Http {
namespace ApiManager {

class Env : public google::api_manager::ApiManagerEnvInterface,
            public Logger::Loggable<Logger::Id::http> {
 private:
  Server::Instance& server;
  Upstream::ClusterManager& cm_;

 public:
  Env(Server::Instance& server)
      : server(server), cm_(server.clusterManager()){};

  virtual void Log(LogLevel level, const char* message) override;
  virtual std::unique_ptr<google::api_manager::PeriodicTimer>
  StartPeriodicTimer(std::chrono::milliseconds interval,
                     std::function<void()> continuation) override;
  virtual void RunHTTPRequest(
      std::unique_ptr<google::api_manager::HTTPRequest> request) override;
  virtual void RunGRPCRequest(
      std::unique_ptr<google::api_manager::GRPCRequest> request) override;
};
}
}
