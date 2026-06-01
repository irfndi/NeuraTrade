#!/usr/bin/env bash
# Generate a self-signed CA + server cert + client cert for local gRPC TLS.
# Use ONLY for development. Production must use a real CA.
set -euo pipefail

CERT_DIR="${CERT_DIR:-./dev-certs}"
DAYS="${DAYS:-825}"
CN="${CN:-localhost}"
ORG="${ORG:-NeuraTrade Dev}"

mkdir -p "$CERT_DIR"

if [[ -f "$CERT_DIR/ca.pem" && -f "$CERT_DIR/server.pem" && -f "$CERT_DIR/client.pem" ]]; then
  echo "Certificates already exist in $CERT_DIR — delete them to regenerate."
  exit 0
fi

echo "Generating CA in $CERT_DIR/ca.pem"
openssl genrsa -out "$CERT_DIR/ca.key" 2048 2>/dev/null
openssl req -x509 -new -nodes -key "$CERT_DIR/ca.key" -sha256 -days "$DAYS" \
  -subj "/CN=${CN}-CA/O=${ORG}" \
  -out "$CERT_DIR/ca.pem"

echo "Generating server cert in $CERT_DIR/server.pem"
openssl genrsa -out "$CERT_DIR/server.key" 2048 2>/dev/null
openssl req -new -key "$CERT_DIR/server.key" \
  -subj "/CN=${CN}/O=${ORG}" \
  -out "$CERT_DIR/server.csr"

cat >"$CERT_DIR/server.ext" <<EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${CN}
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

openssl x509 -req -in "$CERT_DIR/server.csr" -CA "$CERT_DIR/ca.pem" -CAkey "$CERT_DIR/ca.key" \
  -CAcreateserial -out "$CERT_DIR/server.pem" -days "$DAYS" -sha256 \
  -extfile "$CERT_DIR/server.ext"

echo "Generating client cert in $CERT_DIR/client.pem"
openssl genrsa -out "$CERT_DIR/client.key" 2048 2>/dev/null
openssl req -new -key "$CERT_DIR/client.key" \
  -subj "/CN=${CN}-client/O=${ORG}" \
  -out "$CERT_DIR/client.csr"

cat >"$CERT_DIR/client.ext" <<EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

openssl x509 -req -in "$CERT_DIR/client.csr" -CA "$CERT_DIR/ca.pem" -CAkey "$CERT_DIR/ca.key" \
  -CAcreateserial -out "$CERT_DIR/client.pem" -days "$DAYS" -sha256 \
  -extfile "$CERT_DIR/client.ext"

rm -f "$CERT_DIR/server.csr" "$CERT_DIR/server.ext" "$CERT_DIR/client.csr" "$CERT_DIR/client.ext" "$CERT_DIR/ca.srl"

chmod 600 "$CERT_DIR"/*.key

echo ""
echo "Done. To use these in dev:"
echo ""
echo "  export NEURATRADE_GRPC_TLS_CA_FILE=$PWD/$CERT_DIR/ca.pem"
echo "  export CCXT_GRPC_TLS_CA_FILE=$PWD/$CERT_DIR/ca.pem"
echo ""
echo "Server cert: $CERT_DIR/server.pem + $CERT_DIR/server.key"
echo "Client cert: $CERT_DIR/client.pem + $CERT_DIR/client.key"
