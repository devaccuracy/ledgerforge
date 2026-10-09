#!/usr/bin/env bash
# Copyright 2026 LedgerForge Contributors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail
go run ./cmd/ledgerforge migrate up
# Typesense has no shell/curl healthcheck dependency in this runner.
for attempt in {1..60}; do
  if response=$(exec 3<>/dev/tcp/localhost/8108
    printf 'GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n' >&3
    cat <&3) 2>/dev/null && [[ "$response" == *'"ok":true'* ]]; then
    break
  fi
  if [[ "$attempt" == 60 ]]; then
    echo 'Typesense did not start within 60 seconds.' >&2
    exit 1
  fi
  sleep 1
done
if [[ "$#" == 0 ]]; then
  set -- -count=1 ./...
fi
exec go test "$@"
