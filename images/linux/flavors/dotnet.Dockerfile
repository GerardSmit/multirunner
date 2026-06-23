# Linux "dotnet" flavor: .NET SDK 8 + 9, with Node inherited from the node flavor.
# Covers ASP.NET Core + JS SPA builds (mcr dotnet/sdk ships no Node; this does).
#
#   docker build -f images/linux/flavors/dotnet.Dockerfile \
#     --build-arg PARENT=multirunner/runner-linux-node:dev -t multirunner/runner-linux-dotnet:dev .
ARG PARENT=gerardsmit/multirunner-runner-linux:node
FROM ${PARENT}

USER root
ENV DOTNET_ROOT=/usr/local/dotnet \
    DOTNET_CLI_TELEMETRY_OPTOUT=1 \
    DOTNET_NOLOGO=1 \
    DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1
ENV PATH=${DOTNET_ROOT}:${DOTNET_ROOT}/tools:${PATH}
# dotnet-install.sh detects the arch (x64/arm64) so this stays multi-arch.
RUN curl -fsSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh \
    && chmod +x /tmp/dotnet-install.sh \
    && /tmp/dotnet-install.sh --channel 8.0 --install-dir "${DOTNET_ROOT}" \
    && /tmp/dotnet-install.sh --channel 9.0 --install-dir "${DOTNET_ROOT}" \
    && rm /tmp/dotnet-install.sh \
    && chmod -R a+rX "${DOTNET_ROOT}"

USER runner
RUN dotnet --list-sdks
