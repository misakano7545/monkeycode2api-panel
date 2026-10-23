# 多阶段构建：静态二进制 + distroless 运行时。
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mc2api ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/mc2api /app/mc2api
EXPOSE 7887
ENTRYPOINT ["/app/mc2api", "-config", "/app/config.json"]
