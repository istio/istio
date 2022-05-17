#include "proxy_wasm_intrinsics.h"

class HeaderInjectorRootContext : public RootContext {
 public:
  explicit HeaderInjectorRootContext(uint32_t id, std::string_view root_id)
      : RootContext(id, root_id) {}

  bool onConfigure(size_t) override;
};

class HeaderInjectorContext : public Context {
 public:
  explicit HeaderInjectorContext(uint32_t id, RootContext* root) : Context(id, root) {}

  FilterHeadersStatus onRequestHeaders(uint32_t, bool) override;
  FilterHeadersStatus onResponseHeaders(uint32_t, bool) override;

 private:
  inline HeaderInjectorRootContext* rootContext() {
    return dynamic_cast<HeaderInjectorRootContext*>(this->root());
  }
};