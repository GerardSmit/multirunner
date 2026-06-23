# Linux "rust" flavor: rustup stable + musl target, with Node inherited from the
# node flavor (napi-rs / wasm-pack / Tauri all need Rust + Node in one job).
#
#   docker build -f images/linux/flavors/rust.Dockerfile \
#     --build-arg PARENT=multirunner/runner-linux-node:dev -t multirunner/runner-linux-rust:dev .
ARG PARENT=gerardsmit/multirunner-runner-linux:node
FROM ${PARENT}

USER root
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y --no-install-recommends musl-tools musl-dev \
    && rm -rf /var/lib/apt/lists/*

# System-wide rustup so the runner user (and any job) can add components/targets.
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo
ENV PATH=${CARGO_HOME}/bin:${PATH}
RUN curl -fsSL https://sh.rustup.rs -o /tmp/rustup-init.sh \
    && sh /tmp/rustup-init.sh -y --no-modify-path --profile minimal --default-toolchain stable \
    && rm /tmp/rustup-init.sh \
    && rustup target add x86_64-unknown-linux-musl \
    && rustup component add clippy rustfmt \
    && chmod -R a+rwX "${RUSTUP_HOME}" "${CARGO_HOME}"

USER runner
RUN rustc --version && cargo --version
