# Backend

Самостоятельный Go-модуль сервера. Dockerfile использует этот каталог как build context; импорты и структура пакетов внутри модуля сохраняются.

```text
backend/
├── cmd/           API, мигратор, CLI и локальный preview
├── internal/      реализация и unit-тесты
├── data/          рабочий банк вопросов
├── migrations/    SQL-миграции
├── testdata/      синтетический каталог
├── Dockerfile
├── .dockerignore
├── go.mod
└── go.sum
```

Из корня репозитория: `make test`, `make smoke`, `make up`. Из `backend/` сборку и unit-тесты можно запустить командой `docker build --target test -t max-carreer-bot-test .`. Сборка закреплена на Go 1.27.1; host Go не требуется.

API-контракт и клиент лежат в общем [api/](../api/). Поведение и хранение описаны в [docs/api.md](../docs/api.md), запуск — в [docs/operations.md](../docs/operations.md).
