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

# Prints the CHANGELOG.md section for a version, without its heading.
# Usage: release-notes.sh <version> [changelog]   (version without a leading "v")
set -euo pipefail

version="${1:?usage: release-notes.sh <version> [changelog]}"
changelog="${2:-CHANGELOG.md}"

notes="$(awk -v version="$version" '
  index($0, "## [" version "]") == 1 { found = 1; next }
  found && /^## \[/ { exit }
  found && /^\[[^]]+\]: / { exit }
  found { print }
' "$changelog")"

if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "release-notes: no CHANGELOG section for version $version" >&2
  exit 1
fi

printf '%s\n' "$notes"
