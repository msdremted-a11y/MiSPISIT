# Системный монитор на базе eBPF + Qt

Инструмент для мониторинга нагрузки Linux-систем: снимки загрузки CPU и
ввода-вывода для системного администратора.

**Ветка `coursework` — курсовая работа.** Версия 0.1.0 — конструктор
eBPF-пробников: пользователь, не зная eBPF, собирает датчик ядра из
смысловых параметров (что наблюдаем, процесс, что считаем, интервал),
система сама генерирует строку мини-языка и скрипт bpftrace, а сервис
`probe-runner` выполняет его в привилегированном контейнере и пишет
замеры в базу.

## Запуск (ветка coursework)

    git clone -b coursework https://github.com/msdremted-a11y/MiSPISIT
    cd MiSPISIT
    docker compose up -d --build

Страницы системы: http://localhost:8080
Конструктор пробников: http://localhost:8080/probes
Панель Grafana: http://localhost:3000 (открывается сразу, без входа)

Требования: Linux-ядро с BTF (Docker Desktop на WSL2 подходит).

## Адреса

| Адрес | Что открывается |
|---|---|
| http://localhost:8080/ | Главная: название, версия, переходы к функциям, уведомлениям и панели |
| http://localhost:8080/snapshots/new | Форма: сохранить снимок нагрузки (из `/proc` или вручную) |
| http://localhost:8080/snapshots | Список сохранённых снимков |
| http://localhost:8080/snapshots/1 | Карточка одного снимка |
| http://localhost:8080/rejected | Отказ: кто пытался сохранить снимок и почему он не принят |
| http://localhost:8080/notifications | Уведомления: сообщения о сохранённых снимках, статус новое/прочитано |
| http://localhost:8080/probes | Пробники eBPF: список датчиков, конструктор, замеры |
| http://localhost:8080/probes/new | Конструктор пробника: собрать датчик ядра без знания eBPF |
| http://localhost:3000 | Панель Grafana «Системный монитор eBPF + Qt — 0.1.0» |

## Конструктор eBPF-пробников (версия 0.1.0, курсовая)

Пользователь выбирает в форме: что наблюдать (чтение из файлов, дисковые
операции, открытие файлов, TCP-соединения), фильтр по процессу, что считать
(количество, байты, средняя задержка) и интервал. Из параметров генерируется
строка мини-языка (`on disk_io where process = postgres every 10s bytes`)
и скрипт bpftrace — он собирается только из проверенных шаблонов, произвольный
код ввести нельзя. Сервис `probe-runner` исполняет скрипт и пишет замеры
в таблицу `probe_sample`; карточка пробника показывает язык, скрипт и замеры.

## Сервисы

| Сервис | Образ | Порт | Назначение |
|---|---|---|---|
| `app` | `troykins/qt-ebpf-monitor:0.1.0` | 8080 | Страницы функций, уведомлений и конструктора (Go, html/template) |
| `db` | `postgres:16` | наружу не открыт | Снимки, отказы, уведомления, пробники и замеры, том `dbdata` |
| `dashboard` | `grafana/grafana:11.4.0` | 3000 | Панель по данным базы, ходит в `db` по имени сервиса |
| `probe-runner` | `troykins/qt-ebpf-monitor-probe-runner:0.1.0` | наружу не открыт | Исполнитель пробников: bpftrace, privileged, pid: host |

Источник данных и экран Grafana лежат в `grafana/provisioning` и подключаются при старте.

## Структура

    cmd/web/main.go                         запуск сервера и адреса страниц
    cmd/probe-runner/main.go                исполнитель пробников: запуск bpftrace, запись замеров
    internal/actions/                       три функции + создание уведомления при сохранении
    internal/probes/                        конструктор: каталог событий, валидация, генерация DSL и bpftrace
    internal/storage/                       работа с PostgreSQL, создание таблиц
    internal/procstat/                      снятие нагрузки из /proc/stat и /proc/diskstats
    templates/, static/                     шаблоны страниц и оформление
    grafana/provisioning/datasources/       источник данных app (PostgreSQL, сервис db)
    grafana/provisioning/dashboards/        экран monitor.json и его подключение
    qt-client/                              окно Qt (клиентский интерфейс)
    Dockerfile                              образ приложения
    Dockerfile.probe-runner                 образ исполнителя пробников (alpine + bpftrace)
    docker-compose.yml                      приложение, база, панель и probe-runner вместе
