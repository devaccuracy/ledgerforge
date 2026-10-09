# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build-env
WORKDIR /go/src/ledgerforge

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -trimpath -o /ledgerforge ./cmd/ledgerforge
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -trimpath -o /ledgerforge-mcp ./cmd/ledgerforge-mcp

FROM postgres:16-bookworm

ENTRYPOINT []

COPY --from=build-env /ledgerforge /usr/local/bin/ledgerforge
COPY --from=build-env /ledgerforge-mcp /usr/local/bin/ledgerforge-mcp
COPY LICENSE.md NOTICE.md /usr/share/licenses/ledgerforge/

CMD ["ledgerforge", "start"]

EXPOSE 5001
