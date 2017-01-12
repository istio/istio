/* Copyright 2017 Google Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#ifndef MIXERCLIENT_CLIENT_IMPL_H
#define MIXERCLIENT_CLIENT_IMPL_H

#include "include/client.h"
#include "src/transport_impl.h"

namespace istio {
namespace mixer_client {

class MixerClientImpl : public MixerClient {
 public:
  // Constructor
  MixerClientImpl(const MixerClientOptions& options);

  // Destructor
  virtual ~MixerClientImpl();

  virtual void Check(const Attributes& attributes, DoneFunc on_done);
  virtual void Report(const Attributes& attributes, DoneFunc on_done);
  virtual void Quota(const Attributes& attributes, DoneFunc on_done);

  // The async call.
  // on_check_done is called with the check status after cached
  // check_response is returned in case of cache hit, otherwise called after
  // check_response is returned from the Controller service.
  //
  // check_response must be alive until on_check_done is called.
  virtual void Check(const ::istio::mixer::v1::CheckRequest& check_request,
                     ::istio::mixer::v1::CheckResponse* check_response,
                     DoneFunc on_check_done);

  // This is async call. on_report_done is always called when the
  // report request is finished.
  virtual void Report(const ::istio::mixer::v1::ReportRequest& report_request,
                      ::istio::mixer::v1::ReportResponse* report_response,
                      DoneFunc on_report_done);

  // This is async call. on_quota_done is always called when the
  // quota request is finished.
  virtual void Quota(const ::istio::mixer::v1::QuotaRequest& quota_request,
                     ::istio::mixer::v1::QuotaResponse* quota_response,
                     DoneFunc on_quota_done);

 private:
  MixerClientOptions options_;
  StreamTransport<::istio::mixer::v1::CheckRequest,
                  ::istio::mixer::v1::CheckResponse>
      check_transport_;
  StreamTransport<::istio::mixer::v1::ReportRequest,
                  ::istio::mixer::v1::ReportResponse>
      report_transport_;
  StreamTransport<::istio::mixer::v1::QuotaRequest,
                  ::istio::mixer::v1::QuotaResponse>
      quota_transport_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(MixerClientImpl);
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_CLIENT_IMPL_H
