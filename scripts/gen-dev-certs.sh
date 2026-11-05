#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p certs
umask 077
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 30 \
  -subj '/CN=AMT development CA' \
  -keyout certs/ca.key -out certs/ca.crt >/dev/null 2>&1
for service in peer exit; do
  openssl req -newkey rsa:2048 -nodes -sha256 \
    -subj "/CN=$service.local" \
    -keyout "certs/$service.key" -out "certs/$service.csr" >/dev/null 2>&1
  cat > "certs/$service.ext" <<EXT
basicConstraints=CA:FALSE
subjectAltName=DNS:$service.local,DNS:localhost,IP:127.0.0.1
extendedKeyUsage=serverAuth
keyUsage=digitalSignature,keyEncipherment
EXT
  openssl x509 -req -sha256 -days 30 -in "certs/$service.csr" \
    -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial \
    -extfile "certs/$service.ext" -out "certs/$service.crt" >/dev/null 2>&1
  rm "certs/$service.csr" "certs/$service.ext"
done
cat > dev.env <<ENV
PEER_TOKEN=$(openssl rand -hex 32)
EXIT_TOKEN=$(openssl rand -hex 32)
ENV
chmod 600 dev.env certs/*.key
printf 'Development certs generated in certs/.\nTokens saved in dev.env (keep private).\n'
