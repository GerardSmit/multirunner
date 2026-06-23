# Linux "native-build" flavor: the build substrate.
# Adds the C/C++ toolchain + python3 that node-gyp, native Python wheels, Rust's
# linker, and most ./configure scripts need. Also the FROM base for node/go/rust.
#
#   docker build -f images/linux/flavors/native-build.Dockerfile \
#     --build-arg PARENT=multirunner/runner-linux:minimal -t multirunner/runner-linux-native-build:dev .
ARG PARENT=gerardsmit/multirunner-runner-linux:minimal
FROM ${PARENT}

USER root
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y --no-install-recommends \
      build-essential cmake ninja-build pkg-config \
      python3 python3-dev python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*

USER runner
