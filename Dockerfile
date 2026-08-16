# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build-env
WORKDIR /go/src/ledgerforge

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -o /ledgerforge ./cmd/ledgerforge

FROM postgres:16-bookworm

ENTRYPOINT []

COPY --from=build-env /ledgerforge /usr/local/bin/ledgerforge

CMD ["ledgerforge", "start"]

EXPOSE 5001
