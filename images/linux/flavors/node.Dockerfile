# Linux "node" flavor: Node.js LTS + corepack (npm/pnpm/yarn).
# Inherits gcc/make/python3 from native-build, so node-gyp native modules build.
#
#   docker build -f images/linux/flavors/node.Dockerfile \
#     --build-arg PARENT=multirunner/runner-linux-native-build:dev -t multirunner/runner-linux-node:dev .
ARG PARENT=gerardsmit/multirunner-runner-linux:native-build
FROM ${PARENT}
ARG NODE_MAJOR=22

USER root
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update -y && apt-get install -y --no-install-recommends nodejs \
    && corepack enable \
    && rm -rf /var/lib/apt/lists/*

USER runner
