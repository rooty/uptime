# TODO

- [x] Графики зелёных и красных проверок.
- [x] Проверки HTTP(s), SMTP(s), IMAP(s), POP3(s), MySQL, Redis(s), RabbitMQ/AMQP(s) и PostgreSQL.
- [x] Индивидуальные интервалы, background worker и ограничение истории.
- [x] TLS handshake, срок действия сертификатов и предупреждение за 21 день.
- [x] Инциденты, повторные уведомления, Telegram, Slack и SMTP email.
- [x] Runbooks и локальные reminders.
- [x] Uptime dashboard, боковой список сервисов, история и SVG uptime badges.
- [x] Cloudflare Access для production-маршрутов.
- [x] systemd, Nginx, Makefile и GitHub Actions deployment templates.
- [x] Domains: MX lookup и диагностика домена.
- [x] Domains: SPF lookup и проверка политики.
- [x] Domains: DKIM lookup по selector.
- [x] Domains: DMARC lookup и проверка политики.
- [x] Domains: проверка IP почтовых серверов по DNSBL/RBL.
- [x] Domains: постоянный мониторинг доменов с индивидуальным интервалом.
- [x] Domains: включение, выключение и удаление постоянных проверок.
- [x] Ручная очистка старой истории проверок.
- [x] Отдельный тест graceful shutdown worker.
- [x] Тесты Cloudflare Access и syntax validation deployment scripts.
- [x] Production smoke test GitHub Actions, Nginx и Cloudflare Access на реальном сервере.
- [x] Проверка доменной регистрации, даты окончания, MX и TLS-сертификатов MX; alert при изменении данных регистратора.

## Будущие задачи

- [ ] Интеграция с Globalping через Go SDK
  [jsdelivr/globalping-go](https://github.com/jsdelivr/globalping-go).
- [ ] Независимые проверки доступности для каждой пары **IP → service**, включая все IPv4 и IPv6,
  полученные при разрешении имени сервиса. Хранить отдельные статус, диагностику и историю каждой пары;
  на карте показывать результат конкретного IP, а не общий статус сервиса. Для HTTP сохранять исходный
  `Host`, для TLS — исходный hostname в SNI и при проверке сертификата.

## Зафиксированные решения

- DNS resolver: системный resolver окружения;
- DKIM: auto-discovery common selectors с возможностью ручного selector;
- история Domains: 500 проверок на домен;
- DNSBL/RBL: Spamhaus ZEN, SpamCop, Barracuda и SORBS;
- Mail health score: текущая формула `warning=-15`, `not_found=-20`, `error=-30`;
- RBAC Cloudflare Access: не добавлять, доступ контролируется Allow Policy;
- Domains: использовать `/uptime/domain`, без отдельного hostname;
- DNSBL: проверять IPv4 и IPv6.
