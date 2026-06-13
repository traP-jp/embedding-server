FROM golang:1.26-trixie

WORKDIR /app/server

ENV GOCACHE=/root/.cache/go-build \
	GOMODCACHE=/go/pkg/mod

RUN --mount=type=cache,target=${GOMODCACHE} \
	--mount=type=cache,target=${GOCACHE} \
	go install github.com/air-verse/air@v1.61.1

COPY go.mod go.sum ./
RUN --mount=type=cache,target=${GOMODCACHE} \
	--mount=type=cache,target=${GOCACHE} \
	go mod download

CMD ["air", "-c", "/app/development/server.air.toml"]
