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
found=0
packages=$(go list ./...)
# Discover targets through Go so build constraints and renamed packages are respected.
while IFS= read -r package; do
  targets=$(go test -short -list '^Fuzz' "$package")
  while IFS= read -r target; do
    [[ "$target" =~ ^Fuzz[A-Za-z0-9_]+$ ]] || continue
    found=1
    go test -short -run '^$' -fuzz "^${target}$" -fuzztime=5s -parallel=2 "$package"
  done <<< "$targets"
done <<< "$packages"
if [[ "$found" == 0 ]]; then
  echo 'No fuzz targets found; smoke job skipped.'
fi
