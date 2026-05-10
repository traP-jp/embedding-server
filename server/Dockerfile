#syntax=docker/dockerfile:1

FROM golang:1.26-trixie AS build
WORKDIR /src

ENV CGO_ENABLED=0 \
	GOCACHE=/root/.cache/go-build \
	GOMODCACHE=/go/pkg/mod

RUN --mount=type=cache,target=${GOMODCACHE} \
	--mount=type=cache,target=${GOCACHE} \
	--mount=type=bind,source=go.mod,target=go.mod \
	--mount=type=bind,source=go.sum,target=go.sum \
	go mod download

RUN --mount=type=cache,target=${GOMODCACHE} \
	--mount=type=cache,target=${GOCACHE} \
	--mount=type=bind,target=. \
	go build -trimpath -ldflags="-s" -o /out/api .

# アップロード画像などのジョブファイルを書き込む /data を nonroot で使えるようにする
FROM debian:trixie-slim AS data-dir
RUN mkdir -p /data && chown -R 65532:65532 /data

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build --chown=65532:65532 /out/api /app/api
COPY --from=data-dir --chown=65532:65532 /data /data

CMD ["/app/api"]
