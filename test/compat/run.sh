#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# The python-client compatibility job (brief S3 §4): build fabric-server,
# start it with a temporary store on a free loopback port, optionally start
# go-fabric's reference bridge as a commissionable device, and run
# test_python_client.py with the pinned matter-python-client.
#
#   PYTHON=/path/to/venv/bin/python test/compat/run.sh
#
# PYTHON is an interpreter with test/compat/requirements.txt installed
# (pip install --require-hashes -r test/compat/requirements.txt). With
# COMPAT_BRIDGE=1 the reference bridge of the go-fabric version in go.mod
# is built from the module cache and commissioned by the test.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
python=${PYTHON:-python3}
work=$(mktemp -d)
pids=()
cleanup() {
	for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

free_port() { "$python" -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }

cd "$root"
CGO_ENABLED=0 go build -o "$work/fabric-server" ./cmd/fabric-server

if [[ "${COMPAT_BRIDGE:-0}" == 1 ]]; then
	gofabric=$(go list -m -f '{{.Version}}' github.com/SukramJ/go-fabric)
	GOBIN="$work" CGO_ENABLED=0 go install "github.com/SukramJ/go-fabric/examples/reference-bridge@$gofabric"
	export FABRIC_SERVER_BRIDGE_PASSCODE=20202024 FABRIC_SERVER_BRIDGE_DISCRIMINATOR=3333
	"$work/reference-bridge" --db "$work/bridge.db" --listen ":$(free_port)" --tcp false \
		--passcode "$FABRIC_SERVER_BRIDGE_PASSCODE" --discriminator "$FABRIC_SERVER_BRIDGE_DISCRIMINATOR" \
		--log-level warn >"$work/bridge.log" 2>&1 &
	pids+=($!)
fi

port=$(free_port)
# The reference bridge carries the CSA test attestation chain:
# --enable-test-net-dcl trusts its roots.
"$work/fabric-server" --listen "127.0.0.1:$port" --storage-path "$work/store" \
	--enable-test-net-dcl --log-level info >"$work/server.log" 2>&1 &
pids+=($!)
for _ in $(seq 1 100); do
	if "$work/fabric-server" --health-check "http://127.0.0.1:$port/health" 2>/dev/null; then break; fi
	sleep 0.2
done
"$work/fabric-server" --health-check "http://127.0.0.1:$port/health"

status=0
FABRIC_SERVER_URL="ws://127.0.0.1:$port/ws" "$python" -m pytest -q -p no:cacheprovider "$root/test/compat/test_python_client.py" || status=$?
if [[ $status -ne 0 ]]; then
	echo "--- server log (tail)"; tail -n 60 "$work/server.log"
	[[ -f "$work/bridge.log" ]] && { echo "--- bridge log (tail)"; tail -n 30 "$work/bridge.log"; }
fi
exit $status
