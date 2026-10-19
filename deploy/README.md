# Production-развёртывание

## Обзор

Этот каталог содержит `systemd` unit и конфигурацию Nginx для запуска приложения из `/opt/snippetbook`.
Nginx принимает HTTP-запросы и передаёт их приложению на `127.0.0.1:8080`. Cloudflare Access
проверяет пользователя на edge, а приложение дополнительно проверяет подписанный JWT.

## Требования

- Linux с `systemd`;
- установленный `nginx`;
- собранный бинарник для целевой архитектуры;
- пользователь Linux `snippetbook`;
- production-домен, направленный на Nginx;
- TLS-сертификат на origin-сервере для production-домена;
- настроенное Cloudflare Access-приложение;
- файл `/opt/snippetbook/.env` с обязательными настройками.

## Шаги

1. Создайте каталог установки. Приложение и systemd-сервис запускаются от `root`:

   ```bash
   sudo mkdir -p /opt/snippetbook
   ```

2. Скопируйте в `/opt/snippetbook` бинарник, каталог `web` и `.env`:

   ```bash
   sudo cp dist/linux-amd64/snippetbook /opt/snippetbook/snippetbook
   sudo cp -R dist/linux-amd64/web /opt/snippetbook/web
   sudo cp .env.example /opt/snippetbook/.env
   sudo chown -R root:root /opt/snippetbook
   sudo chmod 750 /opt/snippetbook/snippetbook
   sudo chmod 600 /opt/snippetbook/.env
   ```

3. Заполните `/opt/snippetbook/.env`:

   ```dotenv
   PORT=8080
   DB_PATH=/opt/snippetbook/app.db
   CLOUDFLARE_ACCESS_TEAM_DOMAIN=https://my-team.cloudflareaccess.com
   CLOUDFLARE_ACCESS_AUDIENCE=application-audience-tag
   ```

   Не добавляйте `.env` в Git и не публикуйте его содержимое.

4. Установите и запустите systemd unit:

   ```bash
   sudo cp deploy/snippetbook.service /etc/systemd/system/snippetbook.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now snippetbook
   sudo systemctl status snippetbook
   ```

5. Выпустите сертификат для production-домена. Для Cloudflare Origin CA:

   - откройте **SSL/TLS → Origin Server**;
   - нажмите **Create Certificate**;
   - добавьте production-домен, например `uptime.example.com`;
   - сохраните certificate в `/etc/ssl/cloudflare/uptime.example.com.pem`;
   - сохраните private key в `/etc/ssl/cloudflare/uptime.example.com.key`;
   - установите владельца `root:root` и права `600` на private key.

   После выпуска сертификата замените `uptime.example.com` в конфигурации Nginx на свой домен.

   Сертификат можно выпустить автоматически скриптом:

   ```bash
   export CLOUDFLARE_API_TOKEN='token-with-ssl-certificates-edit'
   export ORIGIN_HOSTNAME='uptime.example.com'
   sudo -E deploy/generate-cloudflare-origin-cert.sh
   ```

   Токен должен иметь право **SSL and Certificates: Edit**. Скрипт сохраняет сертификат и ключ
   в `/etc/ssl/cloudflare`, проверяет их соответствие и не выводит private key в журнал.

   Для автоматической проверки и перезагрузки Nginx используйте:

   ```bash
   sudo -E RELOAD_NGINX=1 deploy/generate-cloudflare-origin-cert.sh
   ```

   Для временного внутреннего тестирования можно создать self-signed certificate:

   ```bash
   export ORIGIN_HOSTNAME='example.com'
   sudo -E deploy/generate-self-signed-cert.sh
   ```

   Self-signed certificate не подходит для Cloudflare **Full (strict)**: Cloudflare не сможет
   подтвердить доверие к origin. Для production используйте Cloudflare Origin CA или Let's Encrypt.

6. Установите конфигурацию Nginx:

   ```bash
   sudo cp deploy/nginx-snippetbook.conf /etc/nginx/conf.d/snippetbook.conf
   sudo nginx -t
   sudo systemctl reload nginx
   ```

7. В Cloudflare откройте **SSL/TLS → Overview** и установите режим **Full (strict)**.

   Страницы ошибок `4xx` и `5xx` для Nginx загружаются с `https://error-pages.example.com`.
   Например, ошибка `404` запрашивает `/404.html`, а ошибка `502` — `/502.html`. Исходный
   HTTP-код сохраняется в ответе клиенту.

8. Проверьте локальный upstream на сервере:

   ```bash
   curl -I http://127.0.0.1:8383/
   ```

   Запрос без Cloudflare Access JWT должен получить `401`, если переменные Access заполнены.

9. Откройте в браузере:

   ```text
   https://uptime.example.com/uptime
   ```

## Пример GitHub Actions

Файл `.github/workflows/deploy.yml.example` содержит пример workflow для тестов и деплоя после push в `main`.
Файл не запускается автоматически, потому что не имеет расширения `.yml` или `.yaml`. Чтобы включить
GitHub Actions, переименуйте его в `.github/workflows/deploy.yml`. В GitHub
создайте environment `production` и добавьте Secrets:

- `DEPLOY_HOST` — IP или DNS-имя сервера;
- `DEPLOY_USER` — пользователь с правом выполнить `sudo`;
- `DEPLOY_SSH_KEY` — приватный SSH-ключ без passphrase либо ключ, доступный runner;
- `DEPLOY_KNOWN_HOSTS` — рекомендуется строка из `ssh-keyscan -H <host>`;
- `DEPLOY_PORT` — необязательно, по умолчанию `22`.

Пользователь деплоя должен существовать на сервере, а `sudo` для него не должен запрашивать пароль
для следующих операций:

```text
systemctl restart snippetbook.service
systemctl is-active snippetbook.service
install/cp/chown в /opt/snippetbook
```

После деплоя workflow заменяет бинарник и содержимое `web`, но не изменяет `.env`, `app.db` и
конфигурацию systemd/Nginx на сервере.

## Решение проблем

- **`snippetbook.service` не запускается** — проверьте `sudo journalctl -u snippetbook -n 100`.
- **`EnvironmentFile` не найден** — создайте `/opt/snippetbook/.env` и установите права `600`.
- **`status=203/EXEC`** — проверьте наличие бинарника и права `chmod 750 /opt/snippetbook/snippetbook`.
- **Nginx возвращает `502 Bad Gateway`** — проверьте `sudo systemctl status snippetbook` и порт `8080`.
- **Ответ `401` после входа через Cloudflare** — проверьте Team Domain, AUD Tag и передачу
  `Cf-Access-Jwt-Assertion` в Nginx. Дополнительная причина проверки записывается в
  `sudo journalctl -u snippetbook -n 100` без вывода самого JWT.
- **Cloudflare возвращает `526`** — проверьте, что Nginx слушает `443`, сертификат не просрочен,
  содержит точное имя production-домена, private key соответствует certificate, а в Cloudflare
  установлен режим **Full (strict)**.
- **Cloudflare возвращает `525`** — проверьте TLS handshake, пути к certificate/key, права на
  private key и наличие открытого порта `443` в firewall.
- **Приложение пишет в неправильную базу** — проверьте абсолютный `DB_PATH` и `WorkingDirectory`.
- **Данные теряются после обновления** — не удаляйте `/opt/snippetbook/app.db` и заменяйте только
  бинарник и каталог `web`.
