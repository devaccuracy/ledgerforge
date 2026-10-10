# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26.9-alpine AS build-env
WORKDIR /go/src/ledgerforge

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o /ledgerforge ./cmd/ledgerforge
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /ledgerforge-mcp ./cmd/ledgerforge-mcp

FROM postgres:16-bookworm

ENTRYPOINT []

COPY --from=build-env /ledgerforge /usr/local/bin/ledgerforge
COPY --from=build-env /ledgerforge-mcp /usr/local/bin/ledgerforge-mcp
COPY LICENSE.md NOTICE.md /usr/share/licenses/ledgerforge/

CMD ["ledgerforge", "start"]

EXPOSE 5001
