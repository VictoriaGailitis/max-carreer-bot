FROM node:22.18.0-bookworm-slim AS build
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY api/client/ /app/api/client/
COPY backend/internal/geography/cities.json /app/backend/internal/geography/cities.json
COPY backend/data/question-bank.v1.json /app/backend/data/question-bank.v1.json
RUN npm run build

FROM caddy:2.11.4-alpine
COPY deploy/caddy/Caddyfile.frontend /etc/caddy/Caddyfile
COPY --from=build /app/frontend/dist /srv/frontend
