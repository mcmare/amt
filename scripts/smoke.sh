#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Self-contained local integration test. Requires Go, OpenSSL, curl, Python 3.
./scripts/gen-dev-certs.sh
set -a
# shellcheck disable=SC1091
source ./dev.env
set +a
go build -o ./amt ./cmd/amt
mkdir -p .smoke
printf 'AMT smoke test reached the local origin\n' > .smoke/index.html
python3 -m http.server 18080 --bind 127.0.0.1 --directory .smoke > .smoke/origin.log 2>&1 & origin_pid=$!
./amt exit --listen 127.0.0.1:19443 --cert certs/exit.crt --key certs/exit.key --allow-private >.smoke/exit.log 2>&1 & exit_pid=$!
./amt peer --listen 127.0.0.1:18443 --exit 127.0.0.1:19443 --cert certs/peer.crt --key certs/peer.key --site-dir site >.smoke/peer.log 2>&1 & peer_pid=$!
./amt client --listen 127.0.0.1:11080 --peers https://127.0.0.1:18444,https://127.0.0.1:18443 --peer-ca certs/ca.crt --exit-ca certs/ca.crt --exit-server-name exit.local >.smoke/client.log 2>&1 & client_pid=$!
cleanup(){ kill "$client_pid" "$peer_pid" "$exit_pid" "$origin_pid" 2>/dev/null || true; wait 2>/dev/null || true; }
trap cleanup EXIT
for _ in $(seq 1 40); do
  if curl --silent --fail --noproxy '*' http://127.0.0.1:18080/ >/dev/null && \
     curl --silent --fail --cacert certs/ca.crt https://127.0.0.1:18443/ >/dev/null; then break; fi
  sleep .2
done
for _ in $(seq 1 15); do
  if curl --silent --fail --max-time 8 --socks5-hostname 127.0.0.1:11080 --noproxy '' \
      http://localhost:18080/ > .smoke/result; then
     if grep -q 'AMT smoke test reached' .smoke/result; then echo 'PASS: SOCKS5 -> peer fallback -> TLS -> HTTP/2 peer -> TLS exit -> HTTP origin';exit 0;fi
  fi
  sleep .3
done
echo 'FAIL: See .smoke/*.log' >&2
for file in .smoke/*.log;do echo "--- $file";tail -20 "$file";done
exit 1
