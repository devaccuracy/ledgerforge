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
example_dir=$(cd "$(dirname "$0")" && pwd)
python3 - "$example_dir/ledgerforge.local.json" <<'PYTHON'
import json
import os
import secrets
import sys

path = sys.argv[1]
if not os.path.exists(path):
    config = {
        "project_name": "LedgerForge local demo",
        "data_source": {"dns": "postgres://postgres:password@postgres:5432/ledgerforge?sslmode=disable"},
        "redis": {"dns": "redis:6379"},
        "server": {"port": "5001", "secure": True, "secret_key": secrets.token_hex(32)},
    }
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as file:
        json.dump(config, file, indent=2)
        file.write("\n")
with open(path) as file:
    config = json.load(file)
if config.get("server", {}).get("secure") is not True or not config["server"].get("secret_key"):
    sys.exit("The local demo requires secure: true and a nonempty secret key.")
PYTHON
docker compose -p ledgerforge-demo -f "$example_dir/compose.yaml" up --build -d --wait
echo "Local-only API ready at http://127.0.0.1:${LEDGERFORGE_API_PORT:-5001}"
echo "Run $example_dir/transfer.sh to verify a balanced transfer."
