# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/kkrepo-go ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kkrepo-go /usr/local/bin/kkrepo-go
ENV KKREPO_ADDR=:8080 \
    KKREPO_DATA=/data
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/kkrepo-go"]
