# Подключение фронтенда

Все команды стенда выполняются из корня общего репозитория. Исходники интерфейса добавляются в `frontend/`, Go-сервер находится в `backend/`. Контракт: [OpenAPI](../api/openapi.yaml); клиент: [api/client/client.ts](../api/client/client.ts).

## 1. Запустить backend

```sh
bash scripts/dev/bootstrap-local.sh  # один раз, если .env и .local/secrets/ ещё отсутствуют
docker compose up --build -d
curl -fsS http://127.0.0.1:18080/healthz
```

`/healthz` проверяет процесс. `/readyz` требует опубликованный реальный каталог и пока возвращает `503`; вход, опросник и профиль при этом работают.

## 2. Выбрать адрес API

Предпочтительный вариант — запросы на `/api/v1` через тот же origin. В dev server настройте proxy `/api` на `http://127.0.0.1:18080`. Например, для Vite:

```ts
import { defineConfig } from 'vite';
export default defineConfig({
  server: {
    proxy: { '/api': { target: 'http://127.0.0.1:18080', changeOrigin: true } },
  },
});
```

Если frontend делает запросы напрямую на другой origin, добавьте в backend `.env`:

```dotenv
CORS_ALLOWED_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
```

Затем `docker compose up -d api`. В клиенте используйте `new NavigatorClient('http://127.0.0.1:18080/api/v1')`. Origins сравниваются точно, без завершающего `/`; `localhost` и `127.0.0.1` — разные origins. В production допускается только HTTPS. По умолчанию список пуст; CORS не требуется при общем origin или dev proxy. Разрешены `Authorization`, `Content-Type`, `Idempotency-Key`; `X-Request-ID` доступен браузеру. Cookies не используются.

## 3. Вход

Скопируйте `api/client/client.ts` в frontend или подключите его как исходный модуль. Он использует стандартный `fetch`, поддерживает `AbortSignal` и не зависит от React/Vue.

```ts
import { NavigatorClient, ApiError } from './client';
const api = new NavigatorClient();

// MAX SDK уже подключён и window.WebApp готов.
await api.login(window.WebApp.initData);
const state = await api.me();
const metadata = await api.questionnaire();
```

Передавайте исходную строку `initData` без изменения и дополнительного декодирования. Тип глобального `window.WebApp` предоставляется frontend-интеграцией MAX. Сервер проверяет подпись и свежесть initData (5 минут), выдаёт bearer-сессию на 12 часов. Клиент хранит её только в памяти; после перезагрузки снова выполняйте вход. Токен бота и ключи backend не передаются фронтенду, включая build-time переменные.

Для разработки в обычном браузере:

```sh
python3 scripts/dev/local-init-data.py
# Или POST сгенерированного JSON сразу в API:
python3 scripts/dev/local-init-data.py | curl -fsS \
  -H 'Content-Type: application/json' --data-binary @- \
  http://127.0.0.1:18080/api/v1/auth/max
```

Скрипт выводит JSON с `init_data`; передайте его значение в `api.login(...)`. Подпись свежая, действует 5 минут и работает только на локальном стенде с соответствующим синтетическим токеном. `--user-id 999000112` создаёт вторую тестовую личность. Настоящие credentials скрипт использовать отказывается. Сгенерированные строки не включайте в сборку и Git.

## 4. Опросник, сохранение и завершение

1. `me()` определяет стартовый экран: профиль отсутствует — onboarding; черновик есть — предложить продолжение; профиль есть — рекомендации.
2. `questionnaire()` даёт направления, специализации, общие вопросы, варианты ответа и шкалу. Для `mobile` по умолчанию используется `mobile-all`, для `management` — `product`.
3. `preview({ directions, profiles })` возвращает точные ID и тексты выбранных вопросов. Сохраняйте ответы по ID из ответа сервера. `null` означает пропуск/затруднение, отсутствие ключа — ещё не отвечено.
4. `saveDraft(revision, draft)` полностью заменяет черновик. Первый `revision` берите из `me().draft_revision` либо `draft().revision` (обычно `0`). После каждого сохранения используйте ревизию ответа. Последовательно сохраняйте изменения, не отправляйте конкурентные autosave.
5. `complete(draftRevision, key)` завершает onboarding. Создайте `key = crypto.randomUUID()` один раз и сохраните вместе с ревизией для повторов при сетевой ошибке. Новый ключ на каждую попытку повторного запроса не создавайте.

```ts
const selection = { directions: ['backend'] as const };
const preview = await api.preview({ directions: [...selection.directions] });
const state = await api.me();
const saved = await api.saveDraft(state.draft_revision, {
  current_step: 6,
  directions: ['backend'],
  goals: ['course'],
  education_stage: 'undergraduate',
  study_year: 2,
  preferred_format: 'online',
  backend_languages: ['go'],
  skills: Object.fromEntries(preview.questions.map(q => [q.id, 0])),
});
const completionKey = crypto.randomUUID();
await api.complete(saved.revision, completionKey);
```

`current_step` — число 1–64, определяемое навигацией UI; для пустого выбора направлений допускается только шаг 1. Для завершения нужны 1–3 направления, цель, этап обучения, формат и ответы на каждый выданный вопрос; для очного формата также город либо `any_city`. `explore` и язык `none` выбираются отдельно. Отправляйте только поля `DraftInput`; версии, `question_ids`, `completed_at` и `base_profile_revision` вычисляет сервер. При повторном редактировании перенесите поля ответов из профиля, задайте текущий шаг и актуальную ревизию черновика.

## 5. Каталог и избранное

После завершения профиля вызывайте `recommendations()`, `opportunities({ type, direction, format })`, `opportunity(id)`. Результат уже отсортирован сервером. Пустая выдача — `200` с `items: []`; сбой каталога — `503`, для него нужен отдельный экран ошибки с повтором.

`favorites()`, `addFavorite(id)`, `removeFavorite(id)` работают с тем же bearer. Для исчезнувшей карточки `effective_status = unavailable` и `item = null`; показывайте возможность удалить её из избранного. Для `closed` отключайте запись. `availability_hint = check_source` означает, что доступность нужно уточнить у источника. Открывайте валидированный `action_url` карточки.

До импорта реального каталога экраны карточек проверяются на frontend mocks либо изолированным `make smoke`. [Синтетический набор](../backend/testdata/catalog-demo.v1.json) содержит вымышленные ссылки и не предназначен для публикации. Backend не подменяет им реальную выдачу.

## 6. Ошибки

`ApiError` содержит `status` и `detail` (`code`, `message`, необязательные `fields`, `request_id`). Сетевые ошибки и отмена остаются стандартными ошибками fetch.

| Ответ | Действие UI |
| --- | --- |
| `401 INVALID_SESSION` | Клиент очищает bearer; повторить вход MAX |
| `401 INIT_DATA_EXPIRED` | Получить свежую строку initData из MAX |
| `409 REVISION_CONFLICT` | Загрузить текущий черновик и согласовать изменения пользователя |
| `409 PROFILE_REQUIRED` | Открыть onboarding |
| `409 PROFILE_OUTDATED` | Предложить обновить профиль |
| `409 OPPORTUNITY_UNAVAILABLE` | Обновить карточку, отключить добавление |
| `422 VALIDATION_ERROR` | Показать ошибки `fields` рядом с полями |
| `503` | Показать недоступность и повтор; не рисовать пустой каталог |

`logout()` отзывает текущую сессию. `discardDraft()` отменяет редактирование, сохраняя активный профиль. `deleteData()` удаляет аккаунт и все его сессии — UI должен отдельно подтверждать это намерение.

## 7. Собранный frontend через Caddy

Добавьте исходники в `frontend/` и соберите интерфейс его штатной командой. Стенд по умолчанию читает `frontend/dist/` с `index.html`. При другой структуре сборки укажите путь в корневой `.env`:

```dotenv
FRONTEND_DIST=./frontend/dist
```

Путь относительно корня общего репозитория; абсолютный путь тоже допустим, но остаётся только в локальной `.env`. Зависимости и настройки сборки интерфейса хранятся в `frontend/`.

```sh
docker compose -f compose.yaml -f deploy/compose.frontend.yaml up --build -d
```

Откройте `http://127.0.0.1:18080`. Caddy обслуживает frontend и SPA fallback; `/api/v1/*`, `/healthz`, `/readyz` идут в backend. Папка сборки смонтирована read-only. В production:

```sh
docker compose -f compose.yaml -f deploy/compose.production.yaml -f deploy/compose.frontend.yaml up --build -d
```

Требования к домену, секретам и MAX — в [runbook](operations.md). Папка `frontend/` пока содержит инструкцию для будущего интерфейса; framework будет определён при добавлении его исходников.
