# Системный монитор на базе eBPF + Qt

Инструмент для мониторинга нагрузки Linux-систем: снимки загрузки CPU и
ввода-вывода для системного администратора.

Версия 0.0.3 — три главные функции хранят данные в PostgreSQL.
Приложение и база поднимаются вместе одной командой.
Окно Qt из версии 0.0.1 остаётся клиентским интерфейсом (папка `qt-client/`).

## Запуск

    Репозиторий: https://github.com/msdremted-a11y/MiSPISIT
    Ветка: lab3
    Pull Request: https://github.com/msdremted-a11y/MiSPISIT/pull/3
    Образ: troykins/qt-ebpf-monitor:0.0.3
    Страница образа: https://hub.docker.com/r/troykins/qt-ebpf-monitor

    git clone -b lab3 https://github.com/msdremted-a11y/MiSPISIT
    cd MiSPISIT
    docker compose up -d

После запуска откройте http://localhost:8080

Проверка сохранности: `docker compose down`, затем снова `docker compose up -d` —
записи на месте, потому что база хранит файлы в томе `dbdata`.

## Адреса

| Адрес | Что открывается |
|---|---|
| `/` | Главная: название, версия, переходы к функциям |
| `/snapshots/new` | Форма: сохранить снимок нагрузки (из `/proc` или вручную) |
| `/snapshots` | Список сохранённых снимков |
| `/snapshots/1` | Карточка одного снимка |
| `/rejected` | Отказ: кто пытался сохранить снимок и почему он не принят |

## Хранение

PostgreSQL 16 (`postgres:16`), отдельный сервис `db`. Таблицы `snapshot` и `rejection`
приложение создаёт само при запуске. Адрес базы приложение берёт из переменной
`DATABASE_URL`, её задаёт `docker-compose.yml`.

## Структура

    cmd/web/main.go              запуск сервера и адреса страниц
    internal/actions/            три функции: сохранить, показать, отклонить
    internal/storage/            работа с PostgreSQL, создание таблиц
    internal/procstat/           снятие нагрузки из /proc/stat и /proc/diskstats
    templates/                   шаблоны страниц (Go html/template)
    static/style.css             оформление
    qt-client/                   окно Qt из ЛР1 (клиентский интерфейс)
    go.mod, go.sum               зависимости Go (драйвер lib/pq)
    Dockerfile                   образ приложения, linux/amd64 и linux/arm64
    docker-compose.yml           приложение и база вместе
