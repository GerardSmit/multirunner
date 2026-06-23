# Linux "go" flavor: Go toolchain on the native-build substrate (cgo works via
# the inherited gcc; pure-Go builds ignore it).
#
#   docker build -f images/linux/flavors/go.Dockerfile \
#     --build-arg PARENT=multirunner/runner-linux-native-build:dev -t multirunner/runner-linux-go:dev .
ARG PARENT=gerardsmit/multirunner-runner-linux:native-build
FROM ${PARENT}
ARG GO_VERSION=1.24.4
# TARGETARCH (amd64|arm64) is supplied by buildx; it matches Go's release arch name.
ARG TARGETARCH

USER root
ENV PATH=/usr/local/go/bin:${PATH}
RUN ARCH="${TARGETARCH:-amd64}" \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz

USER runner
RUN go version
