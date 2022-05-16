FROM rust:1.60-buster as builder
WORKDIR /
RUN rustup target add wasm32-unknown-unknown
COPY Cargo.lock ./
COPY Cargo.toml ./
RUN mkdir -p src && touch src/lib.rs
RUN cargo fetch
COPY src/lib.rs src/lib.tmpl

ARG INJECTION_HEADER_VALUE="unknown"
RUN sed -e "s/{{ .VERSION-PLACEHOLDER }}/${INJECTION_HEADER_VALUE}/" src/lib.tmpl > ./src/lib.rs
RUN cargo build --release --target wasm32-unknown-unknown

FROM scratch
WORKDIR /
COPY --from=builder target/wasm32-unknown-unknown/release/plugin.wasm /plugin.wasm
