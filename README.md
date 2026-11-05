# AMT — Adaptive Mesh Transport

**AMT 0.1 (экспериментальная версия)** — собственный транспорт TCP-соединений по цепочке **SOCKS5 → HTTPS/HTTP2 Peer → TLS Exit → Интернет**. Реализация написана на Go и не использует сторонние Go-модули.

> [!WARNING]
> Это **технический прототип**, а не готовый к промышленной эксплуатации VPN. Не доказана работоспособность в сетях российских провайдеров или устойчивость к ТСПУ/DPI. Протокол не гарантирует незаметность, анонимность и обход любых блокировок. Настоящей P2P-сети и автоматического обнаружения узлов в версии 0.1 пока нет.

## Содержание

1. [Схема работы](#схема-работы)
2. [Что реализовано](#что-реализовано)
3. [Быстрая локальная проверка](#быстрая-локальная-проверка)
4. [Установка на два сервера Debian](#установка-на-два-сервера-debian)
5. [Настройка клиента Windows и Linux](#настройка-клиента-windows-и-linux)
6. [Проверка и диагностика](#проверка-и-диагностика)
7. [Все параметры запуска](#все-параметры-запуска)
8. [Безопасность и ограничения](#безопасность-и-ограничения)
9. [Разработка и дальнейшие этапы](#разработка-и-дальнейшие-этапы)

## Схема работы

```text
Браузер / приложение
    |
    | SOCKS5 с удалённым DNS, 127.0.0.1:1080
    v
AMT Client (Windows/Linux)
    |
    | настоящий HTTPS, HTTP/2, токен PEER_TOKEN
    v
AMT Peer (VPS №1, peer.example.com:443)
    |
    | пересылает байты на строго заданный Exit
    v
AMT Exit (VPS №2, :9443)
    |
    | выполняет DNS и исходящее TCP-подключение
    v
Интернет / целевой сайт

Внутри HTTP/2-потока: сквозной TLS 1.3 от Client до Exit.
Peer не имеет ключа внутреннего TLS и не получает адрес назначения
из внутреннего сообщения CONNECT.
```

**Роли:** `client` — локальный SOCKS5; `peer` — HTTPS-релей; `exit` — выход в интернет. Все роли запускаются одним бинарным файлом `amt`. Для удалённого подключения желательно **два отдельных сервера Debian**, находящихся в нужных сетях, и локальный клиент Windows/Linux.

## Что реализовано

- SOCKS5 **CONNECT только для TCP**, с передачей имени домена до Exit (при настройке SOCKS5h/remote DNS).
- Настоящий TLS 1.3 и HTTP/2 от Client до Peer; поверх потока — отдельный сквозной TLS 1.3 до Exit.
- Независимые секреты `PEER_TOKEN` и `EXIT_TOKEN` (не менее 32 символов; рекомендуется `openssl rand -hex 32`).
- Peer передаёт трафик **только заранее заданному Exit** и по желанию раздаёт обычные файлы сайта по HTTPS.
- Проверка сертификатов Peer и Exit; отдельная доверенная CA для Exit.
- Несколько адресов Peer через запятую; переход к следующему при **открытии нового соединения**, если текущий недоступен.
- Ограничение одновременных подключений и запрет обращений к частным/локальным адресам на Exit по умолчанию.
- Запуск нативно или через Docker Compose, локальный интеграционный smoke-тест.

**Нет:** WebRTC, NAT traversal, автоматического P2P-discovery, TUN, UDP, бесшовного восстановления существующих TCP-сессий, встроенной авторизации пользователей SOCKS5 и полноценного аудита безопасности.

## Быстрая локальная проверка

Потребуются **Go 1.23+**, **OpenSSL**, **curl**, **Python 3**. Под Linux/Debian:

```bash
sudo apt update
sudo apt install -y git curl openssl python3 golang-go
# Если версия Go из apt слишком старая, установите Go 1.23+ отдельно.
git clone https://github.com/mcmare/amt.git
cd amt
go test ./...
bash scripts/smoke.sh
```

Скрипт создаёт **временные тестовые сертификаты** в `certs/`, токены в `dev.env`, поднимает локальный HTTP-сайт, Exit, Peer и Client, проверяет резервный Peer и передачу HTTP-ответа через SOCKS5. Успех:

```text
PASS: SOCKS5 -> peer fallback -> TLS -> HTTP/2 peer -> TLS exit -> HTTP origin
```

> Скрипт использует `--allow-private`, чтобы Exit мог обратиться к `localhost` **только во время теста**. Не переносите этот флаг на общедоступный сервер! Файлы `dev.env`, `.env` и каталог `certs/` не должны попадать в Git.

## Установка на два сервера Debian

Ниже приведён **пример развёртывания через Docker Compose**. Замените домен, IP-адреса и имена сертификатов на свои. Предполагается:

| Устройство | Роль | Пример |
| --- | --- | --- |
| VPS №1 | Peer | `peer.example.com`, публичный TCP/443 |
| VPS №2 | Exit | `EXIT_PUBLIC_IP`, TCP/9443 **только от Peer** |
| ПК | Client | `127.0.0.1:1080` (SOCKS5) |

### Шаг 1. Установить Docker и получить исходники

На **каждом** VPS установите Docker Engine с Compose plugin по [официальной инструкции для Debian](https://docs.docker.com/engine/install/debian/). Проверьте:

```bash
docker --version
docker compose version
```

Затем на каждом сервере:

```bash
git clone https://github.com/mcmare/amt.git
cd amt
cp .env.example .env
mkdir -p certs
```

### Шаг 2. Создать два случайных токена

На доверенном компьютере или одном VPS выполните **две отдельные** команды:

```bash
openssl rand -hex 32  # для PEER_TOKEN
openssl rand -hex 32  # для EXIT_TOKEN
```

Скопируйте полученные **разные** строки в `.env` **на обоих VPS**:

```dotenv
PEER_TOKEN=<64-символьная-случайная-hex-строка-1>
EXIT_TOKEN=<64-символьная-случайная-hex-строка-2>
EXIT_ADDRESS=EXIT_PUBLIC_IP:9443
PEER_PORT=443
EXIT_PORT=9443
STREAM_PATH=/_edge/stream
```

Значение `EXIT_ADDRESS` важно на **Peer**: укажите доступный ему адрес Exit (IP либо DNS). В этом MVP Peer только пересылает байты и устанавливает обычный TCP-канал к Exit, а шифрование между Client и Exit выполняется независимо внутри. Не добавляйте `.env` в Git и не вставляйте токены в публичные логи и скриншоты.

### Шаг 3. Подготовить сертификат HTTPS для Peer

У домена `peer.example.com` должна быть A/AAAA-запись на VPS №1. Для публичного домена можно использовать сертификат Let's Encrypt. Например, при свободном TCP/80:

```bash
sudo apt install -y certbot
sudo certbot certonly --standalone -d peer.example.com
```

Скопируйте цепочку сертификатов и приватный ключ в каталог репозитория **на Peer**:

```bash
sudo cp /etc/letsencrypt/live/peer.example.com/fullchain.pem certs/peer.crt
sudo cp /etc/letsencrypt/live/peer.example.com/privkey.pem certs/peer.key
```

Если порт 80 занят, используйте DNS-01 challenge или другую поддерживаемую Certbot-проверку. Скопированные файлы **не обновляются автоматически** при продлении Let's Encrypt — настройте hook, который синхронизирует `peer.crt`/`peer.key` и перезапускает контейнер.

### Шаг 4. Создать приватную CA и сертификат Exit

На **VPS №2** выпустите отдельную CA и серверный сертификат для внутреннего имени `exit.amt.internal`:

```bash
cd ~/amt
umask 077
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
  -keyout certs/exit-ca.key -out certs/exit-ca.crt \
  -subj '/CN=AMT Exit private CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign'

openssl req -new -newkey rsa:3072 -nodes -sha256 \
  -keyout certs/exit.key -out certs/exit.csr \
  -subj '/CN=exit.amt.internal'

printf '%s\n' \
  'basicConstraints=CA:FALSE' \
  'subjectAltName=DNS:exit.amt.internal' \
  'extendedKeyUsage=serverAuth' \
  'keyUsage=digitalSignature,keyEncipherment' > certs/exit.ext

openssl x509 -req -sha256 -days 365 \
  -in certs/exit.csr -CA certs/exit-ca.crt \
  -CAkey certs/exit-ca.key -CAcreateserial \
  -extfile certs/exit.ext -out certs/exit.crt
rm -f certs/exit.csr certs/exit.ext
```

**Только `certs/exit-ca.crt` (публичный сертификат CA)** нужно безопасно передать на клиентский ПК, например сохранить как `exit-ca.crt`. **Никогда не передавайте `exit-ca.key` или `exit.key` на клиент/Peer**.

### Шаг 5. Дать контейнерам доступ к ключам

Контейнеры запускаются под непривилегированным пользователем `uid 10001`. На **каждом сервере** после подготовки файлов:

```bash
sudo chgrp -R 10001 certs
sudo chmod 750 certs
sudo find certs -type f -name '*.key' -exec chmod 640 {} \;
sudo find certs -type f -name '*.crt' -exec chmod 644 {} \;
chmod 600 .env
```

Никогда не публикуйте содержимое `certs/` и `.env`. Для каталогов, содержащих приватные ключи CA, рекомендуются отдельное хранение и резервное копирование вне VPS.

### Шаг 6. Запустить Exit (VPS №2)

```bash
cd ~/amt
docker compose --profile exit up -d --build
docker compose --profile exit ps
docker compose --profile exit logs --tail=100
```

На сетевом экране VPS и у хостинг-провайдера разрешите **TCP/9443 только от IP-адреса Peer**. Например при использовании UFW:

```bash
sudo ufw allow from PEER_PUBLIC_IP to any port 9443 proto tcp
```

Убедитесь, что широкого правила `allow 9443 from anywhere` нет. Проверьте отдельно IPv6 и правила облачного firewall. Docker может управлять собственными iptables/nftables правилами; **не рассчитывайте только на UFW** для опубликованного контейнерного порта — безопаснее дополнительно ограничить доступ в firewall хостинг-провайдера либо Docker `DOCKER-USER`.

### Шаг 7. Запустить Peer (VPS №1)

Проверьте в `.env` значение `EXIT_ADDRESS=EXIT_PUBLIC_IP:9443`, затем:

```bash
cd ~/amt
docker compose --profile peer up -d --build
docker compose --profile peer ps
docker compose --profile peer logs --tail=100
```

Откройте для клиентов **TCP/443**. С обычной машины проверьте стандартный HTTPS-сайт, который отдаётся Peer:

```bash
curl -I https://peer.example.com/
```

Заглушка сайта лежит в `site/index.html`. Её можно заменить своей легитимной страницей. HTTP/2-транспорт использует путь `/_edge/stream` (или `STREAM_PATH`); одинаковое значение требуется у Peer и Client. Корректный HTTPS сам по себе **не гарантирует** отсутствие блокировки.

## Настройка клиента Windows и Linux

### Windows (PowerShell)

Скачайте готовый `amt-windows-amd64.exe` из релиза, если бинарники опубликованы, либо соберите из исходников (`GOOS=windows GOARCH=amd64 go build -o amt.exe ./cmd/amt`). Сохраните рядом файл `exit-ca.crt`, переданный с Exit VPS.

В PowerShell задайте секреты **только на время текущего сеанса**:

```powershell
$env:PEER_TOKEN = 'ВАШ_PEER_TOKEN'
$env:EXIT_TOKEN = 'ВАШ_EXIT_TOKEN'
.\amt.exe client --listen 127.0.0.1:1080 --peers https://peer.example.com --exit-ca .\exit-ca.crt --exit-server-name exit.amt.internal
```

Если бинарник называется `amt-windows-amd64.exe`, подставьте это имя вместо `amt.exe`. Для HTTPS Peer с публичным сертификатом `--peer-ca` не нужен.

В браузере установите **SOCKS5 `127.0.0.1`, порт `1080`**, по возможности включите **Proxy DNS / Remote DNS**. В Firefox это настройка «Проксировать DNS при использовании SOCKS v5». Без удалённого DNS браузер может отправлять DNS-запросы напрямую.

### Linux

```bash
go build -o amt ./cmd/amt
export PEER_TOKEN='ВАШ_PEER_TOKEN'
export EXIT_TOKEN='ВАШ_EXIT_TOKEN'
./amt client --listen 127.0.0.1:1080 \
  --peers https://peer.example.com \
  --exit-ca ./exit-ca.crt \
  --exit-server-name exit.amt.internal
```

Для **нескольких Peer** через запятую:

```bash
./amt client --peers https://peer1.example.com,https://peer2.example.com \
  --exit-ca ./exit-ca.crt --exit-server-name exit.amt.internal
```

Все Peer должны принимать одинаковый `PEER_TOKEN` и путь потока, пересылать на доверенный Exit, использовать проверяемые HTTPS-сертификаты. **Резервирование касается только новых соединений**.

## Проверка и диагностика

С клиентского компьютера:

```bash
curl --socks5-hostname 127.0.0.1:1080 --noproxy '' https://example.com/
```

Проверить внешний IP (вывод должен соответствовать адресу выхода Exit, если сервис доступен):

```bash
curl --socks5-hostname 127.0.0.1:1080 --noproxy '' https://api.ipify.org
```

Проверить Peer независимо от AMT:

```bash
curl -v --http2 https://peer.example.com/
```

Посмотреть логи на VPS:

```bash
docker compose --profile peer logs -f --tail=100    # VPS №1
docker compose --profile exit logs -f --tail=100    # VPS №2
```

Типовые проблемы:

| Симптом | Возможная причина и действие |
| --- | --- |
| `authentication failed` | Не совпали `PEER_TOKEN` либо `EXIT_TOKEN`; проверьте конфигурацию на соответствующих узлах. |
| `x509: certificate signed by unknown authority` | Передайте корректный `exit-ca.crt`; для Peer проверьте публичный сертификат и цепочку. |
| `certificate is valid for ...` | `--exit-server-name` не совпадает с DNS SAN сертификата Exit. |
| `peer returned HTTP 404` | Неверный токен, путь `--stream-path` или запрос не использует HTTP/2. |
| `peer returned HTTP 502` | Peer не может соединиться с `EXIT_ADDRESS`; проверьте сеть/firewall/порт. |
| `destination unavailable or prohibited` | Exit заблокировал внутренний адрес или не смог установить TCP-соединение. |
| Сайт Peer открывается, но SOCKS5 не работает | Обычный HTTPS не проверяет внутренний канал; просмотрите логи Client, Peer, Exit. |
| Нет доступа к локальным сайтам/устройствам через SOCKS | По умолчанию Exit блокирует частные и служебные IP — ожидаемое поведение. |

## Все параметры запуска

Узнать версию или справку:

```bash
./amt version
./amt client --help
./amt peer --help
./amt exit --help
```

| Роль | Параметр | Назначение |
| --- | --- | --- |
| client | `--listen` | Локальный SOCKS5, по умолчанию `127.0.0.1:1080`. |
| client | `--peers` | HTTPS-адреса Peer через запятую. |
| client | `--peer-token` | Токен Peer; по умолчанию из `PEER_TOKEN`. |
| client | `--exit-token` | Токен Exit; по умолчанию из `EXIT_TOKEN`. |
| client | `--peer-ca` | CA для нестандартного сертификата HTTPS Peer. |
| client | `--peer-server-name` | Имя для проверки TLS-сертификата Peer. |
| client | `--exit-ca` | CA для проверки внутреннего TLS Exit. |
| client | `--exit-server-name` | **Обязательное** имя в сертификате Exit. |
| client, peer | `--stream-path` | Путь HTTP/2-потока, по умолчанию `/_edge/stream`. |
| client | `--allow-public-socks` | Разрешает внешнюю привязку SOCKS5 без аутентификации: **опасно**, не использовать публично. |
| peer | `--listen` | HTTPS-сокет Peer, по умолчанию `:8443`. |
| peer | `--cert`, `--key` | Сертификат/ключ публичного HTTPS. |
| peer | `--exit` | Фиксированный TCP-адрес Exit. |
| peer | `--site-dir` | Каталог обычного HTTPS-сайта. |
| exit | `--listen` | TLS-сокет Exit, по умолчанию `:9443`. |
| exit | `--cert`, `--key` | Сертификат/ключ внутреннего TLS. |
| exit | `--allow-private` | Позволяет выход на частные адреса: **только локальное тестирование**. |

## Безопасность и ограничения

1. **Не анонимайзер.** Peer видит IP клиента. Exit видит адрес назначения. Сайт видит IP Exit. Уровень защиты не сопоставим с Tor.
2. **Не «неуловимый» протокол.** IP Peer, TLS/HTTP2-метаданные, длительность и размеры потоков доступны анализу; ТСПУ может ограничить Peer по IP или поведению.
3. **Нельзя открывать SOCKS5 всему интернету.** В MVP SOCKS5 не требует пароля, поэтому слушает только loopback. Не включайте `--allow-public-socks` без отдельного контроля доступа.
4. **Защита Exit.** По умолчанию есть блокировка приватных/служебных IP; дополнительно ограничьте входящие подключения firewall и задайте квоты на уровне хоста. Это не полный аудит SSRF/DoS.
5. **Секреты и CA.** Используйте отдельные случайные токены, не публикуйте ключи и регулярно перевыпускайте сертификаты. Тестовые сертификаты `gen-dev-certs.sh` не применяйте в интернете.
6. **Только TCP.** UDP и TUN не поддерживаются; не все приложения работают через SOCKS5. Настройка прокси в браузере не перенаправляет автоматически весь системный трафик.
7. **Переподключение.** При сбое Peer уже открытая TCP-сессия обрывается. Переключение на резервный Peer происходит при следующем подключении.
8. **Нужны независимые проверки.** Нагрузочное тестирование, долгие соединения, DNS/IPv6-утечки, защита от злоупотреблений и тесты реальных сетей остаются открытыми задачами.

## Разработка и дальнейшие этапы

Структура:

```text
cmd/amt/main.go              точка входа и параметры команд
internal/amt/client.go      SOCKS5-клиент и выбор Peer
internal/amt/peer.go        HTTPS/HTTP2-релей
internal/amt/exit.go        выходной узел и фильтрация назначений
internal/amt/stream.go      двусторонний HTTP/2-поток
internal/amt/protocol.go    сообщения внутреннего протокола
internal/amt/socks5.go      разбор SOCKS5
internal/amt/tls.go         TLS-конфигурация
internal/amt/*_test.go      модульные тесты
scripts/                   тестовые сертификаты и smoke-тест
site/                      пример обычного сайта Peer
Dockerfile                  сборка контейнера
 docker-compose.yml         запуск Peer и/или Exit
```

Разработка:

```bash
go fmt ./...
go test ./...
go test -race ./...
bash scripts/smoke.sh
GOOS=linux GOARCH=amd64 go build -o amt-linux-amd64 ./cmd/amt
GOOS=windows GOARCH=amd64 go build -o amt-windows-amd64.exe ./cmd/amt
```

План следующих версий: метрики успешных соединений и производительности; разделение транспортного интерфейса; проверка WebRTC/Pion как альтернативы; управляемое обнаружение доверенных Peer; квоты, управление ключами, диагностика сетевых блокировок, полноценное тестирование отказов и безопасности.

**Лицензия:** MIT (`LICENSE`).
