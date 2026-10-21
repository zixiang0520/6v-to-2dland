# 6v520 → 2dland 离线下载助手

一个把 [6v520](http://www.6v520.com) 上的磁力链资源**搜索 → 选择 → 一键推送到 [2dland](https://drive.2dland.cn/) 离线下载**的 Web 工具，并自动按「**分类 / 标题(年份) / 第N季**」三级目录归档到你的 2dland 网盘。借助 TMDB 规范化标题与年份，剧集季数自动识别。

后端 Go 单二进制（前端经 `go:embed` 内嵌），Docker 一键部署，约 **16 MB** 镜像即跑。

---

## ✨ 功能特性

- 🔍 **高效搜索**：直接调用 6v520 站内搜索接口（GBK 编码），相比逐分类爬列表页，请求数从 ~88 降到 2，耗时从 8s+ 降到 ~2s。
- 🧲 **磁力链提取**：抓取详情页所有 `magnet:` 链接，手动勾选要推送的条目。
- 📁 **自动归档目录**：推送时自动在 2dland 网盘建目录——
  - 电影：`<根目录>/<分类>/<标题> (<年份>)`
  - 剧集：`<根目录>/<分类>/<标题> (<年份>)/<第N季>`
  - 季数从磁力链文件名/资源标题解析（支持 `S01E05`、`Season 3`、`第N季` 等格式）。
- 🎬 **TMDB 规范化**：用 TMDB 把杂乱的资源标题（如 `2026科幻惊悚《灵魂伴侣》`）规范化为 `灵魂伴侣 (2026)`；TMDB 不可用时自动回退原始标题，不影响推送。
- 🔐 **UI 访问鉴权**：访问密码 + HttpOnly Cookie 会话；首次启动走初始化向导（设密码 + 填 2dland 凭证 + TMDB）。
- ⚙️ **设置热更新**：2dland 凭证 / TMDB / 站点 / 根目录等在设置页改完即时生效，无需重启或改 config 文件。
- 🌐 **2dland 设备码登录**：UI 内完成 OAuth 设备码授权，token 持久化到挂载目录，容器重启免重新授权。
- 📋 **任务管理**：查看 2dland 离线下载队列与进度，单条任务可🗑删除（调 2dland API 同步，可选同时删文件）；支持**批量整理**（勾选多条一次整理）与🔎**探测已整理**（标出哪些任务已归档）。
- 🤖 **自动整理**：任务下载完成后自动清理广告文件并把视频按集数搬进标题目录（后台轮询触发，无需手动点）。
- 📝 **整理记录**：自动/手动/批量整理都会留痕（持久化到 `organize_logs.json`，最近 500 条）：任务名（BT 原始名）、整理前路径、解析出的标题目录，以及**逐条「原文件名 → 新文件名」映射**——整理错了也能查回原来的题目叫什么；支持关键词筛选、单条删除与一键清空。
- 🏠 **发现页**：首页推荐 + 11 个分类「近 N 日」（默认 10 天），整页全抓 + 后端内存缓存；支持勾选多条**批量下载**。
- 🗂️ **网盘文件管理**：浏览 2dland 目录、新建/重命名/移动（拖拽）/删除，查看回收站与恢复，以及全局「最近文件」排查丢失文件。
- ⭐ **关注**：把感兴趣的资源加为关注（持久化到 `follows.json`，容器重启不丢），一键重抓 6v 页面标题刷新名称。
- 🚦 **飞牛影视 / OpenList 联动**：接入飞牛影视（fnOS）API 与 OpenList/AList，用媒体库模糊搜索定位已有剧目录。
- 🛡️ **频控自愈**：2dland 返回 429 后进入冷却期并拒绝推送，避免反复触发风控；可一键**清除限流状态**手动重试。
- 🌓 **暗色模式**：自动跟随系统 / 手动切换。
- 🐳 **Docker 部署**：多阶段构建静态二进制 + 极简 alpine 运行时，单容器 + 一个挂载目录即跑。

---

## 🏗️ 工作原理

```
┌──────────┐   站内搜索    ┌──────────┐   设备码 OAuth   ┌──────────┐
│  6v520   │ ◀─────────── │   本工具   │ ──────────────▶ │  2dland  │
│ (GBK 站) │   GBK 编码   │  (Go+UI)  │   离线下载 API  │  网盘     │
└──────────┘              └────┬─────┘                  └────┬─────┘
                               │ userfile API 建目录          │
                               │ offline_task/add            │
                               ▼                              ▼
                         ┌──────────┐              ┌───────────────────┐
                         │   TMDB   │ ◀── 代理 ─── │  分类/标题(年份)/季 │
                         │ 标题规范化│              └───────────────────┘
                         └──────────┘
```

**数据流**：浏览器 → 本工具后端 → 6v520 搜索/抓详情页 → 前端展示磁力链 → 用户勾选 → 后端调 TMDB 规范化标题 → 在 2dland 建三级目录 → 调 2dland 离线下载接口把磁力链下到该目录。

**整理留痕**：任务下载完成后由 watcher 轮询触发自动整理（删广告 + 按集数规范命名 + 归档到「已下载」）。每次整理——无论自动、任务页手动 ✨ 还是批量/转移已下载——都会追加一条整理记录：触发方式、任务名（BT 原始名）、整理前路径、解析出的标题目录与季数、逐条「原文件名 → 新文件名」映射、删除与跳过清单，失败时连错误一起记。整理把文件改名后原始信息就只剩这条记录，所以「📝 整理记录」页可以按任务名/原标题/文件名/路径直接筛出对应的那一次整理。记录落在 `organize_logs.json`，最多保留 500 条。

---

## 🚀 快速开始

### 方式一：Docker Compose（推荐）

1. 克隆仓库：
   ```bash
   git clone https://github.com/zixiang0520/6v-to-2dland.git
   cd 6v-to-2dland
   ```

2. 准备数据目录（config.json 可先不建，首次启动 UI 会引导）：
   ```bash
   mkdir -p ./data
   ```

3. 启动：
   ```bash
   docker compose up -d --build
   ```

4. 浏览器打开 `http://<服务器IP>:8080`，完成初始化向导（设访问密码 + 填 2dland 凭证 + 可选 TMDB）。

> 端口默认 `8080:8080`，在 [docker-compose.yml](docker-compose.yml) 改 `ports` 即可，如 `28080:8080`。

### 方式二：拉取 GHCR 镜像

每次推送到 `main` 或打 `v*` 标签，GitHub Actions 自动构建并推送镜像到 GHCR：

```yaml
# docker-compose.yml 改用镜像：
services:
  6v-to-2dland:
    image: ghcr.io/zixiang0520/6v-to-2dland:latest   # 私有仓库，需先 docker login ghcr.io
    container_name: 6v-to-2dland
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes: ["./data:/app/data"]
```

```bash
docker login ghcr.io   # 用有 read:packages 权限的 PAT
docker compose up -d
```

### 方式三：直接跑二进制

GitHub Actions 会交叉编译 `linux/amd64` 二进制，在 [Releases](https://github.com/zixiang0520/6v-to-2dland/actions) 下载 artifact（名为 `6v-to-2dland-linux-amd64`）：

```bash
mkdir -p ./data && cd ./data    # config.json / token.json 都以相对路径解析
../6v-to-2dland                  # 启动，监听 :8080
```

> 更详细的部署、反向代理、TMDB 代理、升级流程见 [DEPLOY.md](DEPLOY.md)。

---

## ⚙️ 配置说明

配置优先走 UI 设置页，`config.json` 是持久化载体。首次启动 UI 会引导你完成，**无需手动编辑文件**。

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `listen` | `:8080` | HTTP 监听地址 |
| `base_dir` | `6v下载` | 2dland 网盘内的根目录名 |
| `max_pages` | `8` | 列表爬取 fallback 时每分类最大翻页 |
| `client_id` | — | 2dland 开放平台 client_id（[获取](https://drive.2dland.cn/)） |
| `client_secret` | — | 2dland 开放平台 client_secret |
| `access_password` | — | UI 访问密码，留空则首次启动走初始化向导 |
| `token_file` | `token.json` | 2dland token 持久化文件 |
| `site_base` | `https://www.6v520.com` | 6v520 站点根（**必须 https**，http 会 301 导致搜索流程错乱） |
| `tmdb_api_key` | — | TMDB API Key，留空则不规范化标题 |
| `tmdb_proxy` | — | 访问 TMDB 的代理，如 `http://127.0.0.1:7890`（国内通常需要） |
| `tmdb_language` | `zh-CN` | TMDB 返回语言 |

> 配置含敏感信息，`config.json` 以 `0600` 权限原子写入；`.gitignore` 已排除 `config.json` / `token.json` / `follows.json` / `organize_logs.json`。运行时数据（`token.json`、`follows.json`、`organize_logs.json`）都与 `token_file` 同目录，挂载住该目录即容器重启不丢。

---

## 🧰 从源码构建

**依赖**：Go 1.26+

```bash
# 直接运行
go run .

# 或构建
go build -o 6v-to-2dland .

# 交叉编译 linux/amd64（模拟 Docker builder）
$env:CGO_ENABLED=0; $env:GOOS="linux"; $env:GOARCH="amd64"
go build -trimpath -ldflags="-s -w" -o dist/6v-to-2dland .

# 测试
go test ./...
```

---

## 📡 API 接口

所有接口（除 `/api/health`、`/api/ui/*`）需先登录（访问密码 Cookie）。

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/health` | 健康检查，返回 `{"ok":true}` |
| `GET` | `/api/ui/session` | 当前 UI 会话状态 |
| `POST` | `/api/ui/login` | UI 登录（密码） |
| `POST` | `/api/ui/logout` | UI 登出 |
| `POST` | `/api/ui/setup` | 首次初始化向导 |
| `GET` | `/api/auth/status` | 2dland 登录状态 |
| `POST` | `/api/auth/login` | 启动 2dland 设备码授权 |
| `GET` | `/api/auth/poll` | 轮询 2dland 授权结果 |
| `POST` | `/api/auth/logout` | 2dland 登出 |
| `GET` | `/api/search?q=` | 搜索 6v520 资源 |
| `GET` | `/api/home?days=10` | 发现页数据（最新页整页全抓 + 11 分类近 N 日，5 分钟缓存） |
| `GET` | `/api/magnets?url=` | 抓取详情页磁力链 |
| `POST` | `/api/push` | 推送磁力链到 2dland 离线下载 |
| `GET` | `/api/tasks` | 列出 2dland 离线任务 |
| `POST` | `/api/tasks/delete` | 删除单个任务（同步 2dland，body: `{identity, delete_files}`） |
| `POST` | `/api/tasks/organize` | 整理任务下载文件到标题目录（body: `{save_path, name?}`，`name` 为任务名，仅用于整理记录回溯） |
| `POST` | `/api/tasks/organize-batch` | 批量整理（body: `{items:[{save_path, name}]}`，返回逐条 ok/error） |
| `POST` | `/api/tasks/probe` | 探测全部任务是否已整理，返回带 `organized` 标记的任务列表 |
| `POST` | `/api/drive/clear-rate-limit` | 清除 2dland 限流冷却状态（429 被拒后可手动重试） |
| `GET` | `/api/organize-logs?limit=200` | 整理记录（最新在前，持久化在 `organize_logs.json`，最多保留 500 条） |
| `POST` | `/api/organize-logs/delete` | 删除整理记录（body: `{ids:[...]}` 或 `{id}`） |
| `POST` | `/api/organize-logs/clear` | 清空全部整理记录（只删记录，不动网盘文件） |
| `GET` | `/api/files?path=/` | 浏览 2dland 网盘目录（省略 `path` 即根目录） |
| `POST` | `/api/files/mkdir` | 新建目录（body: `{parent, name}`） |
| `POST` | `/api/files/rename` | 重命名（body: `{identity, name}`） |
| `POST` | `/api/files/move` | 移动文件/目录，供拖拽用（body: `{identities, dest}`） |
| `POST` | `/api/files/delete` | 删除文件/目录（body: `{identities}`） |
| `GET` | `/api/files/trash` | 列出 2dland 回收站 |
| `POST` | `/api/files/recover` | 从回收站恢复（body: `{identities}`） |
| `GET` | `/api/files/recent` | 最近更新的文件（全局，用于排查丢失文件） |
| `GET` | `/api/follows` | 列出关注的资源（持久化在 `follows.json`） |
| `POST` | `/api/follows` | 新增关注（body: `{keyword, title, category, sixv_url}`，`keyword` 必填） |
| `POST` | `/api/follows/delete` | 删除关注（body: `{id}`） |
| `POST` | `/api/follows/rename` | 重抓 6v 页面标题刷新关注名称（body: `{id}`） |
| `GET` | `/api/settings` | 读取设置 |
| `POST` | `/api/settings` | 保存设置（热更新） |
| `POST` | `/api/settings/test` | TMDB 连接测试 |

---

## 🗂️ 项目结构

```
6v-to-2dland/
├── main.go                      # 入口，go:embed 前端
├── internal/
│   ├── cfg/config.go            # 配置结构与原子读写
│   ├── site6v/                  # 6v520 爬虫（GBK 站内搜索 + 详情页磁力链）
│   │   ├── client.go            #   HTTP 客户端（cookie jar 共享频控 cookie）
│   │   ├── list.go              #   站内搜索 /e/search/index.php + 列表 fallback
│   │   ├── home.go              #   发现页抓取（整页全抓 + 分类近 N 日 + 内存缓存）
│   │   ├── detail.go            #   详情页磁力链提取（goquery）
│   │   └── types.go             #   Resource / Magnet
│   ├── tmdb/client.go           # TMDB 客户端（走代理，失败回退原标题）
│   ├── drive/                   # 2dland 客户端（官方 SDK）
│   │   ├── client.go            #   快照 + mutex 热更新凭证
│   │   ├── auth.go              #   OAuth 设备码登录
│   │   ├── folder.go            #   三级目录创建（normalizePath 防御非完整路径）
│   │   ├── offline.go           #   离线下载推送 / 任务列表 / 删除
│   │   ├── watcher.go           #   任务完成轮询 → 触发自动整理（并回调整理结果）
│   │   ├── organize.go          #   下载完成整理（按集数搬进标题目录）
│   │   ├── files.go             #   网盘文件管理（列表/新建/重命名/移动/删除/回收站/恢复）
│   │   └── season.go            #   季数解析正则
│   ├── fnos/client.go           # 飞牛影视（fnOS）API 客户端
│   ├── openlist/client.go       # OpenList/AList 客户端（媒体库模糊搜索定位剧目录）
│   └── server/                  # HTTP 服务
│       ├── server.go            #   路由 + 鉴权中间件 + 会话
│       ├── handlers.go          #   各接口 handler
│       ├── organize_log.go      #   整理记录存储（organize_logs.json）+ 接口
│       └── clear_rate_limit.go  #   清除 2dland 频控限流状态
├── web/                         # 前端（hash 路由 SPA：home/search/tasks/organize-logs/follows/files/settings）
│   ├── index.html
│   ├── style.css                #   暗色模式 + CSS 变量
│   └── app.js                   #   交互逻辑
├── Dockerfile                   # 多阶段构建（golang:1.26-alpine → alpine:3.24）
├── docker-compose.yml           # 一键部署
├── .github/workflows/
│   ├── ci.yml                   # Go test/build + Docker 冒烟测试 + artifact
│   └── ghcr.yml                 # 自动构建推送镜像到 GHCR
├── config.example.json          # 配置示例
└── DEPLOY.md                    # 详细部署文档
```

---

## 🔧 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.26、`net/http`（Go 1.22+ 路由模式）、`embed` |
| 2dland SDK | [halalcloud/golang-sdk-lite](https://github.com/halalcloud/golang-sdk-lite)（HMAC-SHA256 签名 + OAuth 设备码） |
| 爬虫 | [PuerkitoBio/goquery](https://github.com/PuerkitoBio/goquery)、`golang.org/x/text`（GBK 解码） |
| TMDB | [TMDB API v3](https://developer.themoviedb.org/docs)（可选，走代理） |
| 前端 | 原生 HTML/CSS/JS（无构建步骤，hash 路由 SPA）、CSS 变量暗色模式 |
| 部署 | Docker 多阶段构建、GitHub Actions CI/CD、GHCR 镜像 |

---

## ❓ 常见问题

<details>
<summary><b>推送后 2dland 里目录只有「第N季」没有分类和标题？</b></summary>

已在 `ensureDir` 加 `normalizePath` 防御：2dland API 偶发返回非完整路径时，统一用 `joinPath` 兜底拼出完整路径。新推送的任务 save_path 会是 `/根目录/分类/标题(年份)/第N季`。旧任务需删除重推。
</details>

<details>
<summary><b>搜索无结果 / 报频控？</b></summary>

6v520 站内搜索要求间隔 ≥3s，程序已内置频控。若仍报「请不要连续提交」，稍等几秒重试。`site_base` 必须是 `https://`（http 会 301 到 https 导致流程错乱）。
</details>

<details>
<summary><b>2dland 推送报 401 / token 失效？</b></summary>

删除挂载目录内的 `token.json`，在 UI 设置页重新点「登录 2dland」走设备码授权。注意：在设置页**改动 Client ID/Secret 会清空旧 token**（凭证变了需重新授权）。
</details>

<details>
<summary><b>TMDB 规范化不生效？</b></summary>

检查设置页「TMDB 连接测试」：确认 `tmdb_api_key` 有效、`tmdb_proxy` 能从容器内访问（Linux 宿主用内网 IP，不要用 `127.0.0.1`；Docker Desktop 用 `host.docker.internal`）。TMDB 不可用时自动回退原标题，不影响推送。
</details>

<details>
<summary><b>任务状态显示「状态10」？</b></summary>

2dland 返回的任务 `status` 字段未在官方文档给出枚举定义，实测 `10` 为已完成。状态着色/按状态批量清除暂未实现（避免猜错枚举值误删），目前支持单条🗑删除。
</details>

更多部署问题见 [DEPLOY.md#常见问题](DEPLOY.md)。

---

## 📄 License

本项目仅供个人学习使用。请遵守 6v520 与 2dland 的服务条款，下载内容请确保拥有合法授权。
