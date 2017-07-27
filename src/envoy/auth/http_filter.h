#pragma once

#include <string>

#include "server/config/network/http_connection_manager.h"

namespace Envoy {
namespace Http {

class JwtVerificationFilter : public StreamDecoderFilter {
 public:
  JwtVerificationFilter();
  ~JwtVerificationFilter();

  // Http::StreamFilterBase
  void onDestroy() override;

  // Http::StreamDecoderFilter
  FilterHeadersStatus decodeHeaders(HeaderMap&, bool) override;
  FilterDataStatus decodeData(Buffer::Instance&, bool) override;
  FilterTrailersStatus decodeTrailers(HeaderMap&) override;
  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override;

 private:
  StreamDecoderFilterCallbacks* decoder_callbacks_;
};

}  // Http
}  // Envoy