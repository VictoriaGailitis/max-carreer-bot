FROM golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS source

ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY data ./data
COPY testdata ./testdata
COPY migrations ./migrations

FROM source AS test
RUN test -z "$(gofmt -l cmd internal)" && go test ./... && go vet ./...

FROM source AS binaries
RUN CGO_ENABLED=0 go build -trimpath -o /out/devapi ./cmd/devapi \
 && CGO_ENABLED=0 go build -trimpath -o /out/ops ./cmd/ops \
 && CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate

FROM scratch AS migrate
WORKDIR /
COPY --from=binaries /out/migrate /migrate
COPY migrations /migrations
COPY --from=source /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT ["/migrate"]

FROM scratch AS devapi
WORKDIR /
COPY --from=binaries /out/devapi /devapi
COPY data/question-bank.v1.json /data/question-bank.v1.json
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/devapi"]

FROM scratch AS ops
WORKDIR /
COPY --from=binaries /out/ops /ops
COPY data/question-bank.v1.json /data/question-bank.v1.json
USER 65532:65532
ENTRYPOINT ["/ops"]

FROM scratch AS authapi
WORKDIR /
COPY --from=binaries /out/api /api
COPY data/question-bank.v1.json /data/question-bank.v1.json
COPY --from=source /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/api"]
