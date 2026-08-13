# syntax=docker/dockerfile:1

# ============================================================================
# Builder —— 编译静态 Linux 二进制（前端资源经 go:embed 内嵌）
# ============================================================================
FROM golang:1.25-alpine AS builder

WORKDIR /src

# 国内镜像加速拉取模块
ENV GOPROXY=https://goproxy.cn,direct

# 先拷依赖清单，利用层缓存
COPY go.mod go.sum ./
RUN go mod download

# 拷源码（含 web/ —— go:embed 需要）
COPY . .

# 静态构建，去除调试信息以缩小体积
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/kdocs-baiduyun .

# ============================================================================
# Runtime —— Chromium 用于渲染金山文档正文
# ============================================================================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata chromium && \
    update-ca-certificates

ENV KDOCS_BROWSER=/usr/bin/chromium

# 工作目录设为 /app/data：config.json 以相对路径解析至此，
# 把宿主目录挂载到 /app/data 即可完成配置注入与 token 持久化。
WORKDIR /app/data
COPY --from=builder /out/kdocs-baiduyun /app/kdocs-baiduyun

EXPOSE 8080
ENTRYPOINT ["/app/kdocs-baiduyun"]
