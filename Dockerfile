# Build on the runner's native arch and cross-compile; running the Go toolchain under QEMU is ~10x slower.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/hermes-operator ./cmd

FROM gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.source="https://github.com/dbslone/hermes-operator"
COPY --from=build /out/hermes-operator /hermes-operator
USER 65532:65532
ENTRYPOINT ["/hermes-operator"]
