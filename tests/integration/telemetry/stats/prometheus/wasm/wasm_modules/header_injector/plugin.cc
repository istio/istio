#include "plugin.h"

#ifndef INJECTION_VERSION
#error INJECTION_VERSION must be defined
#endif // INJECTION_VERSION

#define str(s) #s

// Boilderplate code to register the extension implementation.
static RegisterContextFactory register_HeaderInjector(CONTEXT_FACTORY(HeaderInjectorContext),
                                               ROOT_FACTORY(HeaderInjectorRootContext));

bool HeaderInjectorRootContext::onConfigure(size_t) { return true; }

FilterHeadersStatus HeaderInjectorContext::onRequestHeaders(uint32_t, bool) {
  addRequestHeader("X-Req-Injection", str(INJECTION_VERSION));
  return FilterHeadersStatus::Continue;
}

FilterHeadersStatus HeaderInjectorContext::onResponseHeaders(uint32_t, bool) {
  addResponseHeader("X-Resp-Injection", str(INJECTION_VERSION));
  return FilterHeadersStatus::Continue;
}