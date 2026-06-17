# Standalone multirunner cache server image.
# Build context = repo root, after cross-compiling bin/cacheserver-linux:
#   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/cacheserver-linux ./cmd/cacheserver
#   docker build -f images/cacheserver/Dockerfile -t multirunner/cacheserver:dev .
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY bin/cacheserver-linux /cacheserver
VOLUME /data
EXPOSE 3000
ENTRYPOINT ["/cacheserver"]
