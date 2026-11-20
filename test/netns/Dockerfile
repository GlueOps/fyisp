# syntax=docker/dockerfile:1.28@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
# Builds the netns harness: the fyisp binary and the compiled test.
# Used by test/netns/run.sh; GO_IMAGE is passed from the repo Dockerfile.
#   docker buildx build -f test/netns/Dockerfile --build-arg GO_IMAGE=... --output type=local,dest=OUT .
ARG GO_IMAGE
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS=linux TARGETARCH=amd64
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath GOOS=$TARGETOS GOARCH=$TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    test -z "$(gofmt -l test/netns | tee /dev/stderr)" \
 && go vet -tags netns ./test/netns \
 && go build -ldflags "-s -w -X main.version=netns-test" -o /out/fyisp ./cmd/fyisp \
 && go test -c -tags netns -o /out/netns.test ./test/netns

FROM scratch
COPY --from=build /out/ /
