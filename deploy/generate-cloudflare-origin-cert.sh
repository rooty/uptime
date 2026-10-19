#!/usr/bin/env bash
# generate-cloudflare-origin-cert.sh — выпускает Cloudflare Origin CA certificate для Nginx.
#
# Обязательные переменные:
#   CLOUDFLARE_API_TOKEN — API Token с правом SSL and Certificates: Edit
#   ORIGIN_HOSTNAME      — hostname из Cloudflare DNS, например example.com
#
# Необязательные переменные:
#   CERT_DIR             — каталог сертификатов, default: /etc/ssl/cloudflare
#   CERT_VALIDITY        — срок в днях: 7, 30, 90, 365, 730, 1095 или 5475
#   RELOAD_NGINX         — 1 для проверки и reload nginx после выпуска
#
# Результат:
#   CERT_DIR/ORIGIN_HOSTNAME.pem — сертификат Cloudflare Origin CA
#   CERT_DIR/ORIGIN_HOSTNAME.key — закрытый ключ

set -euo pipefail

CERT_DIR="${CERT_DIR:-/etc/ssl/cloudflare}"
CERT_VALIDITY="${CERT_VALIDITY:-5475}"
RELOAD_NGINX="${RELOAD_NGINX:-0}"
API_URL="https://api.cloudflare.com/client/v4/certificates"

log() {
    echo "$(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"
}

fail() {
    echo "error: $*" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "не найдена команда: $1"
}

require_command curl
require_command jq
require_command openssl

: "${CLOUDFLARE_API_TOKEN:?задайте CLOUDFLARE_API_TOKEN в окружении}"
: "${ORIGIN_HOSTNAME:?задайте ORIGIN_HOSTNAME, например example.com}"

if [[ ! "$ORIGIN_HOSTNAME" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]]; then
    fail "ORIGIN_HOSTNAME содержит недопустимые символы"
fi

case "$CERT_VALIDITY" in
    7|30|90|365|730|1095|5475) ;;
    *) fail "CERT_VALIDITY должен быть одним из: 7, 30, 90, 365, 730, 1095, 5475" ;;
esac

if [[ "$RELOAD_NGINX" != "0" && "$RELOAD_NGINX" != "1" ]]; then
    fail "RELOAD_NGINX должен быть 0 или 1"
fi

if [[ "$EUID" -ne 0 && "$CERT_DIR" == /etc/* ]]; then
    fail "для записи в $CERT_DIR запустите скрипт через sudo"
fi

mkdir -p "$CERT_DIR"
chmod 700 "$CERT_DIR"

key_file="$CERT_DIR/$ORIGIN_HOSTNAME.key"
cert_file="$CERT_DIR/$ORIGIN_HOSTNAME.pem"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/cloudflare-origin-cert.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT

key_tmp="$work_dir/origin.key"
csr_tmp="$work_dir/origin.csr"
request_tmp="$work_dir/request.json"
response_tmp="$work_dir/response.json"
cert_tmp="$work_dir/origin.pem"
openssl_config="$work_dir/openssl.cnf"

cat > "$openssl_config" <<EOF
[ req ]
prompt = no
distinguished_name = subject
req_extensions = req_ext

[ subject ]
CN = $ORIGIN_HOSTNAME

[ req_ext ]
subjectAltName = DNS:$ORIGIN_HOSTNAME
EOF

log "генерирую private key и CSR для $ORIGIN_HOSTNAME"
openssl req \
    -new \
    -newkey rsa:2048 \
    -nodes \
    -keyout "$key_tmp" \
    -out "$csr_tmp" \
    -config "$openssl_config" \
    >/dev/null 2>&1

jq -n \
    --rawfile csr "$csr_tmp" \
    --arg hostname "$ORIGIN_HOSTNAME" \
    --argjson validity "$CERT_VALIDITY" \
    '{csr: $csr, hostnames: [$hostname], request_type: "origin-rsa", requested_validity: $validity}' \
    > "$request_tmp"

log "запрашиваю Origin CA certificate в Cloudflare"
curl --silent --show-error --fail-with-body \
    --request POST "$API_URL" \
    --header "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
    --header 'Content-Type: application/json' \
    --data-binary "@$request_tmp" \
    > "$response_tmp"

if ! jq -e '.success == true and (.result.certificate // "") != ""' "$response_tmp" >/dev/null; then
    jq -r 'if (.errors | length) > 0 then .errors[] | "Cloudflare API: [\(.code)] \(.message)" else "Cloudflare API вернул неуспешный ответ" end' "$response_tmp" >&2
    exit 1
fi

jq -r '.result.certificate' "$response_tmp" > "$cert_tmp"

log "проверяю certificate и private key"
openssl x509 -in "$cert_tmp" -noout -subject -issuer -dates >/dev/null

cert_hash="$(openssl x509 -in "$cert_tmp" -pubkey -noout | openssl pkey -pubin -outform pem | sha256sum | awk '{print $1}')"
key_hash="$(openssl pkey -in "$key_tmp" -pubout -outform pem | sha256sum | awk '{print $1}')"
if [[ "$cert_hash" != "$key_hash" ]]; then
    fail "certificate и private key не соответствуют друг другу"
fi

install -o root -g root -m 644 "$cert_tmp" "$cert_file"
install -o root -g root -m 600 "$key_tmp" "$key_file"

log "сертификат сохранён: $cert_file"
log "private key сохранён: $key_file"
openssl x509 -in "$cert_file" -noout -subject -issuer -dates

if [[ "$RELOAD_NGINX" == "1" ]]; then
    require_command nginx
    nginx -t
    systemctl reload nginx
    log "nginx перезагружен"
fi
