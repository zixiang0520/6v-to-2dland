# Docker 部署指南

将「6v520 → 2dland 离线下载助手」打包为单容器镜像，前端资源已通过 `go:embed` 内嵌进二进制，部署只需一个镜像 + 一个挂载目录。

## 1. 镜像特点

- **多阶段构建**：builder 用 `golang:1.25-alpine` 编译，runtime 用 `alpine:3.20`，最终镜像约 20MB。
- **静态二进制**：`CGO_ENABLED=0`，无动态库依赖。
- **CA 证书 + tzdata**：容器内访问 TMDB / 2dland 的 HTTPS 接口必需。
- **配置与 token 外挂**：`config.json`、`token.json` 均以相对路径解析到工作目录 `/app/data`，挂载宿主目录即可完成注入与持久化，镜像本身不含任何密钥。

## 2. 准备配置

在宿主机任选一目录（例：`/opt/6v`），放入 `config.json`：

```bash
mkdir -p /opt/6v
cp config.example.json /opt/6v/config.json
vi /opt/6v/config.json
```

按需填写：

```json
{
  "listen": ":8080",
  "base_dir": "6v下载",
  "max_pages": 8,
  "client_id": "你的 2dland client_id",
  "client_secret": "你的 2dland client_secret",
  "token_file": "token.json",
  "site_base": "http://www.6v520.com",
  "tmdb_api_key": "你的 TMDB API Key（留空则不规范化标题）",
  "tmdb_proxy": "",
  "tmdb_language": "zh-CN"
}
```

> 说明：容器内 `token_file` 为相对路径，最终落到 `/app/data/token.json`，与 `config.json` 同目录，重启容器自动复用，无需再次授权。

## 3. 方式 A：docker compose（推荐）

```bash
# 仓库根目录已有 docker-compose.yml，把其中的 ./data 指向你的配置目录，
# 或直接把配置放进 ./data：
mkdir -p ./data && cp /opt/6v/config.json ./data/config.json

docker compose up -d --build
docker compose logs -f
```

停止：`docker compose down`

## 4. 方式 B：docker run

### 4.1 本地构建镜像

```bash
docker build -t 6v-to-2dland:latest .
```

### 4.2 启动容器

```bash
docker run -d \
  --name 6v-to-2dland \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /opt/6v:/app/data \
  6v-to-2dland:latest
```

查看日志 / 停止：

```bash
docker logs -f 6v-to-2dland
docker stop 6v-to-2dland && docker rm 6v-to-2dland
```

## 5. 反向代理（可选）

容器只暴露 HTTP 8080。生产环境建议前置 Nginx 做 HTTPS：

```nginx
server {
    listen 443 ssl;
    server_name 2dland-helper.example.com;

    ssl_certificate     /etc/ssl/cert.pem;
    ssl_certificate_key /etc/ssl/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## 6. 首次使用

1. 浏览器打开 `http://<服务器IP>:8080`。
2. 页面提示「未登录」→ 点击登录，按设备码流程在 2dland 完成授权。
3. 授权成功后 `token.json` 自动写入挂载目录，**容器重启无需再次授权**。
4. 搜索 → 勾选磁力链 → 推送到 2dland，自动按 `分类/标题(年份)/第N季` 建目录。

## 7. 升级

```bash
git pull
docker build -t 6v-to-2dland:latest .
docker rm -f 6v-to-2dland
docker run -d ... 6v-to-2dland:latest   # 参数同上
# 或 compose：
docker compose up -d --build
```

`config.json` 与 `token.json` 在挂载目录内，升级不丢。

## 8. 关于 TMDB 代理

- 容器**默认不走代理**直连 TMDB；若服务器可直连 TMDB，`tmdb_proxy` 留空即可。
- 若服务器无法直连 TMDB，需通过宿主机代理访问：
  - 代理监听宿主端口（如 `7890`），`config.json` 设 `"tmdb_proxy": "http://<宿主内网IP>:7890"`。
  - Linux 宿主：用宿主内网 IP，不要用 `127.0.0.1`（容器内 127.0.0.1 指容器自身）。
  - Docker Desktop（Mac/Win）：可用 `http://host.docker.internal:7890`。
- TMDB 不可用时程序自动回退原始标题，不影响推送。

## 9. 常见问题

| 现象 | 排查 |
|------|------|
| 容器启动后立即退出 | `docker logs 6v-to-2dland` 看报错；多为 `config.json` 缺失或 JSON 格式错误 |
| 推送报 401 / token 失效 | 删除挂载目录内 `token.json`，重新走设备码授权 |
| TMDB 规范化不生效 | 检查 `tmdb_api_key` 是否填写、`tmdb_proxy` 能否从容器内访问 |
| 搜索无结果 | 6v520 站点改版或网络不通；适当调大 `max_pages` |
| 端口被占用 | 改 `-p 9090:8080` 并同步改 `config.json` 的 `listen` 为 `:8080`（容器内不变） |
