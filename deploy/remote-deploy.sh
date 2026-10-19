#!/usr/bin/env bash
# remote-deploy.sh — устанавливает архив SnippetBook на production-сервере.
# Usage: remote-deploy.sh <release-archive> <install-directory>

set -euo pipefail

ARCHIVE_PATH="${1:?укажите путь к архиву релиза}"
INSTALL_DIR="${2:?укажите каталог установки}"

fail() {
    echo "error: $*" >&2
    exit 1
}

[[ -f "$ARCHIVE_PATH" ]] || fail "архив не найден: $ARCHIVE_PATH"
[[ "$INSTALL_DIR" = /* ]] || fail "каталог установки должен быть абсолютным путем"

sudo -n true 2>/dev/null || fail "пользователь деплоя должен выполнять sudo без запроса пароля"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/snippetbook-deploy.XXXXXX")"
cleanup() {
    rm -rf "$work_dir"
}
trap cleanup EXIT

tar -xzf "$ARCHIVE_PATH" -C "$work_dir"
[[ -x "$work_dir/snippetbook" ]] || fail "в архиве отсутствует исполняемый файл snippetbook"
[[ -d "$work_dir/web" ]] || fail "в архиве отсутствует каталог web"

sudo install -d -o root -g root "$INSTALL_DIR"
sudo install -d -o root -g root "$INSTALL_DIR/web"
sudo install -o root -g root -m 750 \
    "$work_dir/snippetbook" "$INSTALL_DIR/snippetbook"
sudo cp -R "$work_dir/web/." "$INSTALL_DIR/web/"
sudo chown -R root:root "$INSTALL_DIR/web"

sudo systemctl restart snippetbook.service
sudo systemctl is-active --quiet snippetbook.service || {
    sudo journalctl -u snippetbook.service -n 50 --no-pager >&2 || true
    fail "snippetbook.service не запустился"
}

echo "SnippetBook успешно обновлён в $INSTALL_DIR"
