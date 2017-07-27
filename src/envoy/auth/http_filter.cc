#include <string>

#include "http_filter.h"

#include "server/config/network/http_connection_manager.h"

namespace Envoy {
namespace Http {

/*
 * TODO: receive issuer's info & pubkey
 */
JwtVerificationFilter::JwtVerificationFilter() {}

JwtVerificationFilter::~JwtVerificationFilter() {}

void JwtVerificationFilter::onDestroy() {}

FilterHeadersStatus JwtVerificationFilter::decodeHeaders(HeaderMap&, bool) {
  /*
   * TODO: verify the JWT in the auth header
   */
  return FilterHeadersStatus::Continue;
}

FilterDataStatus JwtVerificationFilter::decodeData(Buffer::Instance&, bool) {
  return FilterDataStatus::Continue;
}

FilterTrailersStatus JwtVerificationFilter::decodeTrailers(HeaderMap&) {
  return FilterTrailersStatus::Continue;
}

void JwtVerificationFilter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  decoder_callbacks_ = &callbacks;
}

}  // Http
}  // Envoy