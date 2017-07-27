#include <string>

#include "http_filter.h"

#include "envoy/registry/registry.h"

namespace Envoy {
namespace Server {
namespace Configuration {

class JwtVerificationFilterConfig : public NamedHttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb createFilterFactory(const Json::Object&,
                                          const std::string&,
                                          FactoryContext&) override {
    return [](Http::FilterChainFactoryCallbacks& callbacks) -> void {
      callbacks.addStreamDecoderFilter(Http::StreamDecoderFilterSharedPtr{
          new Http::JwtVerificationFilter()});
      /*
       * TODO: pass issuer's info & pubkey to the filter through config JSON
       */
    };
  }
  std::string name() override { return "jwt-auth"; }
  HttpFilterType type() override { return HttpFilterType::Decoder; }
};

/**
 * Static registration for this JWT verification filter. @see RegisterFactory.
 */
static Registry::RegisterFactory<JwtVerificationFilterConfig,
                                 NamedHttpFilterConfigFactory>
    register_;

}  // Configuration
}  // Server
}  // Envoy