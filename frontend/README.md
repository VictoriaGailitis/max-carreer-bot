# Frontend

Каталог для будущих исходников мини-приложения MAX. Добавляйте сюда frontend-проект в этом же Git-репозитории: код, его package manifest, lockfile и конфигурацию сборки. Вложенный `.git` не нужен.

После добавления frontend этот README можно заменить его инструкцией сборки. Framework и менеджер пакетов определяются при переносе интерфейса.

## Интеграция

- Контракт: [api/openapi.yaml](../api/openapi.yaml).
- Готовый TypeScript-клиент: [api/client/client.ts](../api/client/client.ts).
- Сценарий входа, опросник, ошибки и dev proxy: [docs/frontend.md](../docs/frontend.md).
- В dev server проксируйте `/api` на `http://127.0.0.1:18080`.
- В production frontend и backend используют общий origin и базовый путь `/api/v1`.

## Собранное приложение

Ожидаемый каталог сборки — `frontend/dist/` с `index.html`. Если frontend собирается в другой каталог, укажите его в корневой `.env` через `FRONTEND_DIST`. Сборки и `node_modules/` исключены из Git.

После сборки frontend выполните из корня репозитория `make up-frontend`: Caddy отдаёт SPA и API на одном адресе. До добавления и сборки интерфейса используйте `make up` для запуска backend.

Frontend хранит только публичные настройки. Токен бота, database URL и ключи шифрования остаются в корневом `.local/secrets/` и не включаются в frontend-переменные или сборку. Для несекретных примеров настроек можно добавить `frontend/.env.example`.
