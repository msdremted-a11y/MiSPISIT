# Системный монитор на базе eBPF + Qt

Инструмент для мониторинга нагрузки Linux-систем: снимки загрузки CPU и
ввода-вывода для системного администратора.

Версия 0.0.4 — к страницам трёх функций добавлена панель Grafana по данным базы.
Приложение, база и панель поднимаются вместе одной командой.
Окно Qt остаётся клиентским интерфейсом (папка `qt-client/`) и в эту версию не входит.

## Запуск

    Репозиторий: https://github.com/msdremted-a11y/MiSPISIT
    Ветка: lab4
    Pull Request: https://github.com/msdremted-a11y/MiSPISIT/pull/4
    Образ: troykins/qt-ebpf-monitor:0.0.4
    Страница образа: https://hub.docker.com/r/troykins/qt-ebpf-monitor

    git clone -b lab4 https://github.com/msdremted-a11y/MiSPISIT
    cd MiSPISIT
    docker compose up -d

Страницы системы: http://localhost:8080
Панель Grafana: http://localhost:3000 (открывается сразу, без входа)

## Адреса

| Адрес | Что открывается |
|---|---|
| http://localhost:8080/ | Главная: название, версия, переходы к функциям и к панели |
| http://localhost:8080/snapshots/new | Форма: сохранить снимок нагрузки (из `/proc` или вручную) |
| http://localhost:8080/snapshots | Список сохранённых снимков |
| http://localhost:8080/snapshots/1 | Карточка одного снимка |
| http://localhost:8080/rejected | Отказ: кто пытался сохранить снимок и почему он не принят |
| http://localhost:3000 | Панель Grafana «Системный монитор eBPF + Qt — 0.0.4» |

## Сервисы

| Сервис | Образ | Порт | Назначение |
|---|---|---|---|
| `app` | `troykins/qt-ebpf-monitor:0.0.4` | 8080 | Страницы трёх функций (Go, html/template) |
| `db` | `postgres:16` | наружу не открыт | Хранение снимков и отказов, том `dbdata` |
| `dashboard` | `grafana/grafana:11.4.0` | 3000 | Панель по данным базы, ходит в `db` по имени сервиса |

Источник данных и экран Grafana лежат в `grafana/provisioning` и подключаются при старте.

## Структура

    cmd/web/main.go                         запуск сервера и адреса страниц
    internal/actions/                       три функции: сохранить, показать, отклонить
    internal/storage/                       работа с PostgreSQL, создание таблиц
    internal/procstat/                      снятие нагрузки из /proc/stat и /proc/diskstats
    templates/, static/                     шаблоны страниц и оформление
    grafana/provisioning/datasources/       источник данных app (PostgreSQL, сервис db)
    grafana/provisioning/dashboards/        экран monitor.json и его подключение
    qt-client/                              окно Qt (клиентский интерфейс)
    Dockerfile                              образ приложения, linux/amd64 и linux/arm64
    docker-compose.yml                      приложение, база и панель вместе
