# git-plus

`git-plus` 是一个用于备份 GitHub 仓库的本地 Web 应用。它可以同步你自己的仓库、组织内可访问的仓库、Star 过的仓库和 Watch 的仓库，并持续记录分支和 tag 的提交变化历史。

它适合用来降低这些风险：

- GitHub 账号、组织或仓库不可用。
- 仓库被删除、改名、归档或权限变化。
- 分支被强推、覆盖或意外删除。
- 你关注的开源仓库发生历史改写后难以追踪。

## 功能概览

1. 在 Web 仪表盘中浏览仓库、同步状态和任务历史。

![Crafted dashboard](images/highlight-1.jpg)

2. 支持备份你可访问的仓库、Star 仓库和 Watch 仓库。

![Backup sources](images/highlight-2.jpg)

3. 记录分支头提交变化历史，而不只是保存最新快照。

![Commit history tracking](images/highlight-3.jpg)

## Runtime Requirements

Local development requires:

- Node.js `>=22`
- pnpm `10.12.2`
- Git

Production can run the built Node server directly, under PM2, or as the Docker image.

## 必要环境变量

启动服务前必须配置两个环境变量：

```bash
export PASSWORD='choose-a-strong-dashboard-password'
export ENCRYPTION_PASSPHRASE='choose-a-stable-random-secret'
```

含义如下：

- `PASSWORD`：Git Plus 仪表盘和 `/api` 的访问密码。浏览器打开页面后，登录框里输入的就是这个值。它不是 GitHub 密码。
- `ENCRYPTION_PASSPHRASE`：用于加密和解密 GitHub token。这个值必须长期固定保存；如果之后换了值，之前保存到 `config.yaml` 里的 token 将无法解密。

可以用下面的命令生成一个随机加密密钥：

```bash
openssl rand -base64 32
```

只用于本机临时开发时，也可以关闭 API 鉴权：

```bash
export PASSWORD='insecure-noauth'
```

不要在真实备份或可被他人访问的环境中使用 `insecure-noauth`。

## Local Development

1. Install dependencies:

```bash
pnpm install
```

2. Configure environment variables:

```bash
export PASSWORD='abc123'
export ENCRYPTION_PASSPHRASE="$(openssl rand -base64 32)"
```

3. Start the development servers:

```bash
pnpm dev
```

`pnpm dev` starts both processes:

- Vite frontend dev server: `http://127.0.0.1:43210`
- Node backend server: `http://localhost:8080`

In development, the Node backend proxies non-`/api` requests to Vite. Runtime data is written to:

```text
./tmpdata
```

打开浏览器访问：

```text
http://localhost:8080
```

The login password is the value of `PASSWORD`, for example `abc123` above.

如果你使用 `direnv`，可以把环境变量写入 `.env`：

```bash
cp .env.example .env
direnv allow
pnpm dev
```

## Production Build

1. Build the frontend and backend:

```bash
pnpm build
```

The build outputs are:

```text
frontend/dist/
dist/server/index.cjs
```

2. Configure stable environment variables:

```bash
export PASSWORD='choose-a-strong-dashboard-password'
export ENCRYPTION_PASSPHRASE='paste-your-stable-secret-here'
```

3. Start the server directly:

```bash
node dist/server/index.cjs --data-dir ./data
```

Or run it with PM2:

```bash
pnpm pm2:start
```

Production runtime data is written to `--data-dir`. The config file is:

```text
<data-dir>/config.yaml
```

The SQLite database and archived repositories are stored in the same data directory.

## Docker

```bash
docker run -d \
  --name git-plus \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  -e PASSWORD='choose-a-strong-dashboard-password' \
  -e ENCRYPTION_PASSPHRASE='paste-your-stable-secret-here' \
  ghcr.io/imsingee/git-plus:latest
```

然后访问：

```text
http://localhost:8080
```

## 如何配置 GitHub 仓库备份

### 1. 准备 GitHub Personal Access Token

进入 GitHub token 页面：

```text
https://github.com/settings/tokens
```

当前应用表单使用的是 `Personal Access Token (classic)`。

推荐权限：

- 只备份公开仓库：可以先使用 `public_repo`。
- 需要备份私有仓库：需要 `repo`。
- 需要访问组织仓库：token 账号必须有对应组织仓库权限。
- 如果组织启用了 SSO，需要在 GitHub 上为这个 token 授权访问对应组织。

不要使用你的 GitHub 登录密码。这里需要的是 GitHub token。

### 2. 添加备份源

启动 Git Plus 后，打开：

```text
http://localhost:8080/config/sources
```

点击 `Add Source`，填写：

- `Platform`：选择 `GitHub`
- `Name`：可选，用于备注，例如 `Personal GitHub`
- `Username`：你的 GitHub 用户名
- `Personal Access Token (classic)`：刚刚创建的 GitHub token

保存后，token 会使用 `ENCRYPTION_PASSPHRASE` 加密写入 `<data-dir>/config.yaml`。

### 3. 选择要备份哪些仓库

在 `Advanced Options` 中可以配置备份范围：

- `Include default accessible repositories`：默认开启。包括你自己的仓库、协作仓库和组织内可访问仓库。
- `Include starred repositories`：备份你 Star 过的仓库。
- `Include watching repositories`：备份你 Watch 的仓库。
- `Only Include Repos`：只备份指定仓库，支持通配符，例如 `my-org/*`、`*-backup`。
- `Exclude Repos`：排除指定仓库，支持通配符，例如 `my-org/private-*`。

如果你只是想备份自己的 GitHub 仓库，保持 `Include default accessible repositories` 开启即可。

### 4. 手动执行第一次同步

打开：

```text
http://localhost:8080/maintenance/tasks
```

点击 `Sync All`。

同步过程会：

1. 从 GitHub API 拉取仓库列表。
2. 根据 include/exclude 规则过滤仓库。
3. 为每个仓库建立或更新本地 bare archive。
4. 拉取分支和 tag。
5. 记录分支/tag 的创建、更新和删除历史。

同步完成后，可以打开：

```text
http://localhost:8080/repos
```

查看仓库列表、仓库详情、分支/tag 状态和变化历史。

### 5. 配置定时同步

打开：

```text
http://localhost:8080/config/cron
```

填写 5 字段 cron 表达式，例如每小时同步一次：

```text
0 * * * *
```

cron 配置会保存到 `<data-dir>/config.yaml`。

## 配置文件说明

运行配置文件位于：

```text
<data-dir>/config.yaml
```

示例：

```yaml
sources:
  - id: src_7f4a2c91b0d4e8f123456789
    name: Personal GitHub
    platform: github
    username: octocat
    token: $encrypted$1$REPLACE_WITH_ENCRYPTED_VALUE
    only_include_repos: []
    exclude_repos: []
    include_defaults: true
    include_starred: false
    include_watching: false
concurrency: 5
max_retry_times: 2
cron: '0 * * * *'
```

通常不需要手动编辑这个文件，建议通过 Web 页面配置。

主要字段：

- `sources`：备份源列表。
- `concurrency`：同步并发数，默认 `5`。
- `max_retry_times`：失败重试次数，默认 `2`。
- `cron`：可选的 5 字段定时同步表达式。

如果必须手动写入 token，不要保存明文 token。可以使用 CLI 生成加密 token：

```bash
printf '%s' 'ghp_xxx' | ENCRYPTION_PASSPHRASE='your passphrase' node dist/server/index.cjs config encrypt-token
```

输出的 `$encrypted$1$...` 可以粘贴到 `config.yaml` 的 `token` 字段。

## Operations Commands

Start the main server:

```bash
node dist/server/index.cjs --data-dir ./data
```

常用参数：

- `--data-dir string`：必填，运行数据目录。
- `--port string` / `PORT`：监听端口，默认 `8080`。

Start with PM2:

```bash
pnpm pm2:start
```

Encrypt a token for manual config editing:

```bash
printf '%s' 'ghp_xxx' | ENCRYPTION_PASSPHRASE='your passphrase' node dist/server/index.cjs config encrypt-token
```

## 开发常用命令

```bash
pnpm dev
```

启动开发环境。

```bash
pnpm build
```

Build the Vite frontend and Node backend bundle.

```bash
pnpm test
```

Run backend and frontend Vitest suites.

```bash
pnpm check:types
```

Run backend and frontend TypeScript type checks.

```bash
pnpm lint
```

运行 ESLint。

```bash
pnpm format
```

运行 Prettier 和 ESLint 自动修复。

如果修改了 protobuf：

```bash
pnpm buf:generate
pnpm buf:lint
```

如果修改了数据库 schema 或 SQL 查询：

```bash
pnpm db:generate
```

## 常见问题

### `PASSWORD` 是什么？从哪里获取？

`PASSWORD` 是你自己设置的 Git Plus 仪表盘密码，不是 GitHub 密码，也不需要从 GitHub 获取。

例如：

```bash
export PASSWORD='abc123'
```

那么浏览器登录 Git Plus 时输入 `abc123`。

### `ENCRYPTION_PASSPHRASE` 可以每次随机生成吗？

开发时可以，但真实备份不建议。它用于解密已经保存的 GitHub token，必须长期固定保存。

如果你第一次启动时用了：

```bash
export ENCRYPTION_PASSPHRASE='secret-a'
```

后面改成：

```bash
export ENCRYPTION_PASSPHRASE='secret-b'
```

旧 token 就无法解密，需要重新添加或替换 token。

### 应该访问 `localhost:8080` 还是 `127.0.0.1:8080`？

默认访问：

```text
http://localhost:8080
```

如果本机已有其他服务占用了 `8080`，可以换端口启动：

```bash
PORT=8090 node dist/server/index.cjs --data-dir ./data
```

或者开发时临时设置：

```bash
PORT=8090 pnpm dev
```

### 添加 source 后为什么还没有仓库？

添加 source 只是保存备份源配置。需要到 `/maintenance/tasks` 点击 `Sync All`，或者配置 cron 定时同步后，仓库数据才会被拉取和归档。
