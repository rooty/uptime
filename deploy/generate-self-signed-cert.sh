#!/usr/bin/env bash
# generate-self-signed-cert.sh — создаёт self-signed TLS-сертификат для Nginx.
#
# Обязательная переменная:
#   ORIGIN_HOSTNAME — hostname сертификата, например example.com
#
# Необязательные переменные:
#   CERT_DIR        — каталог сертификатов, default: /etc/ssl/cloudflare
#   CERT_VALIDITY   — срок действия в днях, default: 365
#   RELOAD_NGINX    — 1 для проверки и reload nginx после генерации
#
# Результат:
#   CERT_DIR/ORIGIN_HOSTNAME.pem — self-signed certificate
#   CERT_DIR/ORIGIN_HOSTNAME.key — закрытый ключ

set -euo pipefail

CERT_DIR="${CERT_DIR:-/etc/ssl/cloudflare}"
CERT_VALIDITY="${CERT_VALIDITY:-365}"
RELOAD_NGINX="${RELOAD_NGINX:-0}"

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

require_command openssl

: "${ORIGIN_HOSTNAME:?задайте ORIGIN_HOSTNAME, например example.com}"

if [[ ! "$ORIGIN_HOSTNAME" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]]; then
    fail "ORIGIN_HOSTNAME содержит недопустимые символы"
fi

if [[ ! "$CERT_VALIDITY" =~ ^[0-9]+$ || "$CERT_VALIDITY" -lt 1 ]]; then
    fail "CERT_VALIDITY должен быть положительным числом дней"
fi

if [[ "$RELOAD_NGINX" != "0" && "$RELOAD_NGINX" != "1" ]]; then
    fail "RELOAD_NGINX должен быть 0 или 1"
fi

if [[ "$EUID" -ne 0 && "$CERT_DIR" == /etc/* ]]; then
    fail "для записи в $CERT_DIR запустите скрипт через sudo"
fi

mkdir -p "$CERT_DIR"
chmod 700 "$CERT_DIR"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/self-signed-cert.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT

key_tmp="$work_dir/origin.key"
cert_tmp="$work_dir/origin.pem"
openssl_config="$work_dir/openssl.cnf"
key_file="$CERT_DIR/$ORIGIN_HOSTNAME.key"
cert_file="$CERT_DIR/$ORIGIN_HOSTNAME.pem"

cat > "$openssl_config" <<EOF
[ req ]
prompt = no
distinguished_name = subject
x509_extensions = cert_ext

[ subject ]
CN = $ORIGIN_HOSTNAME

[ cert_ext ]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:$ORIGIN_HOSTNAME
EOF

log "генерирую self-signed certificate для $ORIGIN_HOSTNAME"
openssl req \
    -x509 \
    -newkey rsa:2048 \
    -sha256 \
    -nodes \
    -days "$CERT_VALIDITY" \
    -keyout "$key_tmp" \
    -out "$cert_tmp" \
    -config "$openssl_config" \
    >/dev/null 2>&1

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
