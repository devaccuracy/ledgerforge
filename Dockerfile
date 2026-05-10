FROM golang:1.25-alpine AS build-env
WORKDIR /go/src/ledgerforge

COPY . .

RUN go build -o /ledgerforge ./cmd/*.go

FROM debian:bullseye-slim

# Install pg_dump version 16
RUN apt-get update && apt-get install -y wget gnupg2 lsb-release && \
    echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list && \
    wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - && \
    apt-get update && \
    apt-get install -y postgresql-client-16 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build-env /ledgerforge /usr/local/bin/ledgerforge

RUN chmod +x /usr/local/bin/ledgerforge

CMD ["ledgerforge", "start"]

EXPOSE 8080
