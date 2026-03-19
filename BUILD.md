# new-api 本地构建与发布说明

## 概述

这份文档基于当前仓库脚本的实际行为整理，目标是解决以下几个常见问题：

- Docker 多阶段构建里，前端 `vite build` 偶发卡住或报 `rpc EOF`
- Go 依赖下载访问官方源不稳定
- 构建、打 tag、推送、远程部署命令分散，不方便重复执行

当前推荐方案不是直接依赖原始多阶段 `Dockerfile` 一把构建，而是使用仓库内的分步脚本：

- [build_local_image.ps1](/D:/github/QuantumNous/new-api/build_local_image.ps1)
- [build_local_image.bat](/D:/github/QuantumNous/new-api/build_local_image.bat)
- [build_and_push_image.ps1](/D:/github/QuantumNous/new-api/build_and_push_image.ps1)
- [Dockerfile.runtime](/D:/github/QuantumNous/new-api/Dockerfile.runtime)

## 当前提交与镜像

- 当前代码提交：`68fef7cb`
- 最近一次已构建并推送的镜像提交：`2182c2eb`
- 当前版本镜像：`akon/new-api:20260320-2182c2eb`
- 当前 `latest`：`akon/new-api:latest`
- 已推送镜像摘要：`sha256:081272bce36b77fbc12b84c5aee0b966609f411ee3781e597aa8d0a5bf7e76dc`

本次提交除了构建脚本与文档整理，还包含以下功能变更：

- OAuth 新用户在开启“兑换码注册”时，不再直接登录，而是跳转到 `/register/redemption` 完成兑换码注册
- 用于注册的兑换码会在注册成功时自动兑换额度
- 删除用户时会同步清理 `user_oauth_bindings`，避免自定义 OAuth 账号再次授权时被旧绑定残留影响

这套流程会先在宿主机编译前后端产物，再由 Docker 只负责最终运行镜像打包，稳定性更高，也更容易定位问题。

---

## 前置条件

- 已安装并启动 Docker Desktop
- 宿主机已安装 Go，且 `go version` 可用
- 宿主机已安装 Bun，或至少已安装 `pnpm` / `npm`
- 可访问宿主机代理端口 `7897`
- 如需推送远程镜像，需先执行 `docker login`

说明：

- 当前脚本会优先使用本机 Bun 构建前端
- 如果 `bun` 不在 PATH 中，脚本也会尝试自动查找常见安装路径
- Docker 只用于最终运行镜像打包，不再用于前端依赖安装和前端编译

---

## 推荐用法

### 1. 只构建本地镜像

```powershell
Set-Location D:\github\QuantumNous\new-api
.\build_local_image.ps1
```

或直接双击：

```text
build_local_image.bat
```

默认 tag 规则为：

```text
yyyyMMdd-<当前 commit 短 SHA>
```

例如：

```text
akon/new-api:20260320-2182c2eb
```

### 2. 构建并推送版本 tag

```powershell
Set-Location D:\github\QuantumNous\new-api
.\build_local_image.ps1 -Push
```

### 3. 构建后额外打上 `latest`

```powershell
Set-Location D:\github\QuantumNous\new-api
.\build_local_image.ps1 -AlsoTagLatest
```

### 4. 构建、推送版本 tag，并同步推送 `latest`

```powershell
Set-Location D:\github\QuantumNous\new-api
.\build_local_image.ps1 -Push -AlsoTagLatest -PushLatest
```

这是远程服务使用 `latest` 时最方便的一种方式。

### 5. 指定自定义 tag

```powershell
Set-Location D:\github\QuantumNous\new-api
.\build_local_image.ps1 -Tag 20260320-mybuild -Push
```

---

## 脚本实际流程

`build_local_image.ps1` 的流程如下：

1. 检查 Docker Desktop 是否就绪，未就绪时按重试次数循环等待
2. 调用 [build_and_push_image.ps1](/D:/github/QuantumNous/new-api/build_and_push_image.ps1) 执行真正的构建
3. 如带 `-Push`，推送版本 tag
4. 如带 `-AlsoTagLatest`，补打 `latest`
5. 如带 `-PushLatest`，推送 `latest`

`build_and_push_image.ps1` 的流程如下：

1. 在宿主机 `web` 目录执行前端构建
2. 优先使用本机 Bun；如果不存在，再回退到 `pnpm` 或 `npm`
3. 在宿主机使用 Go 交叉编译 `linux/amd64` 二进制到 `out/new-api`
4. 使用 [Dockerfile.runtime](/D:/github/QuantumNous/new-api/Dockerfile.runtime) 打包最终运行镜像
5. 如带 `-Push`，推送版本 tag 到 Docker Hub

这样做的好处：

- 前端编译不再依赖 `docker run bun`
- 可以直接看到本机 Bun、Go、Docker 各阶段日志
- 构建失败时更容易定位是前端、Go 编译，还是镜像打包或镜像推送

---

## 代理配置

脚本默认使用以下代理：

- 宿主机前端 / Go：`http://127.0.0.1:7897`
- 宿主机 SOCKS：`socks5://127.0.0.1:7897`
- `docker push` 阶段也会显式继承宿主机代理 `127.0.0.1:7897`
- Docker 构建阶段 HTTP/HTTPS：`http://host.docker.internal:7897`
- Docker 构建阶段 SOCKS：`socks5://host.docker.internal:7897`
- Go 模块代理：`https://goproxy.cn,direct`

如果你的代理地址不同，可以这样覆盖：

```powershell
Set-Location D:\github\QuantumNous\new-api
powershell -ExecutionPolicy Bypass -File .\build_and_push_image.ps1 `
  -Tag 20260320-mybuild `
  -HostProxy http://127.0.0.1:7897 `
  -HostAllProxy socks5://127.0.0.1:7897 `
  -ContainerProxy http://host.docker.internal:7897 `
  -ContainerAllProxy socks5://host.docker.internal:7897 `
  -GoProxy https://goproxy.cn,direct `
  -Push
```

---

## 镜像检查

构建完成后检查本地镜像：

```powershell
docker images akon/new-api
```

检查某个具体 tag：

```powershell
docker image inspect akon/new-api:20260320-2182c2eb
```

检查 `latest` 是否已更新：

```powershell
docker image inspect akon/new-api:latest
```

---

## 远程 Compose 部署

远程 `docker-compose.yml` 或 `compose.yaml` 建议使用：

```yaml
services:
  new-api:
    image: akon/new-api:latest
```

每次发布完成后，在远程服务器部署目录执行：

```bash
docker compose pull new-api
docker compose up -d --force-recreate new-api
```

如果远程仍然使用老版命令：

```bash
docker-compose pull new-api
docker-compose up -d --force-recreate new-api
```

检查当前实际运行的镜像：

```bash
docker inspect new-api --format='{{.Config.Image}}'
```

查看运行日志：

```bash
docker compose logs -f --tail=200 new-api
```

---

## 常见问题

### 1. `Browserslist: browsers data is 10 months old`

这通常只是警告，不是阻塞构建的根因。

如果日志停在：

```text
transforming...
Browserslist: browsers data (caniuse-lite) is 10 months old
```

更常见的真实问题往往是：

- Docker Desktop / Docker Engine 短暂不稳定
- 网络抖动导致依赖下载或镜像推送失败
- Bun / npm 拉包阶段网络超时

如果只想更新 Browserslist 数据，可以在 `web` 目录执行：

```powershell
bunx update-browserslist-db@latest
```

或：

```powershell
npx update-browserslist-db@latest
```

### 2. `bun` 明明装了，但脚本提示找不到

当前脚本会优先：

1. 从 PATH 查找 `bun`
2. 查找常见安装位置，例如 `C:\Users\admin\.bun\bin\bun.exe`

如果仍找不到，先手动确认：

```powershell
C:\Users\admin\.bun\bin\bun.exe --version
```

### 3. `go mod download` 访问官方源失败

当前脚本默认已使用：

```text
GOPROXY=https://goproxy.cn,direct
```

通常不需要额外修改。

### 4. `docker push` 过程中出现 `EOF`

这通常更像是 Docker Desktop 到 Docker Hub 的网络链路或代理抖动，不是脚本参数错误。

优先检查：

- Docker Desktop 是否正常
- `docker login` 是否已登录
- 代理是否可用
- 当前网络是否能稳定访问 Docker Hub

### 5. Docker API 返回 500

例如：

```text
request returned 500 Internal Server Error for API route ... /docker_engine/_ping
```

通常说明 Docker Desktop 还没完全启动，或者 Docker Engine 短暂异常。

先执行：

```powershell
docker version
docker buildx ls
docker run --rm hello-world
```

确认 Docker 正常后再执行构建。

---

## 相关文件

- 构建入口脚本：[build_local_image.ps1](/D:/github/QuantumNous/new-api/build_local_image.ps1)
- 批处理入口：[build_local_image.bat](/D:/github/QuantumNous/new-api/build_local_image.bat)
- 实际构建脚本：[build_and_push_image.ps1](/D:/github/QuantumNous/new-api/build_and_push_image.ps1)
- 运行镜像 Dockerfile：[Dockerfile.runtime](/D:/github/QuantumNous/new-api/Dockerfile.runtime)
- 原始多阶段 Dockerfile：[Dockerfile](/D:/github/QuantumNous/new-api/Dockerfile)

---

## 文档变更记录

### v1.1.0 - 2026-03-20 00:00

**变更原因：** 同步当前实际构建方式，补充本地构建、打 tag、推送远程镜像的最新用法。

**修改内容：**

1. 移除“前端使用 Bun 容器构建”的旧描述，改为“宿主机优先使用 Bun 构建前端”。
2. 补充 `build_local_image.ps1` 的 `-Push`、`-AlsoTagLatest`、`-PushLatest` 用法。
3. 将固定 tag 示例改为按日期和 commit 自动生成的实际规则。
4. 补充远程 Compose 使用 `latest` 的部署建议。

### v1.2.0 - 2026-03-20 01:30

**变更原因：** 同步本次已提交并已推送的镜像版本，同时补充这版镜像包含的功能变更，方便按文档直接核对部署对象。

**修改内容：**

1. 将文档中的示例镜像 tag 更新为 `akon/new-api:20260320-2182c2eb`。
2. 补充当前版本镜像 `akon/new-api:20260320-2182c2eb`、`akon/new-api:latest` 与镜像摘要 `sha256:081272bce36b77fbc12b84c5aee0b966609f411ee3781e597aa8d0a5bf7e76dc`。
3. 记录本次镜像内实际包含的功能：
   - OAuth 新用户在开启兑换码注册时跳转兑换码注册页
   - 注册型兑换码在注册成功时自动兑换
   - 删除用户时同步清理 `user_oauth_bindings`，支持自定义 OAuth 账号重新授权

### v1.2.1 - 2026-03-20 02:00

**变更原因：** 补充删除用户清理 OAuth 绑定的后续提交，并区分“当前代码提交”和“最近一次已推送镜像提交”，避免文档中的提交号与镜像状态混淆。

**修改内容：**

1. 将“当前代码提交”更新为 `68fef7cb`。
2. 新增“最近一次已构建并推送的镜像提交”为 `2182c2eb`。
3. 保留当前已推送镜像 `akon/new-api:20260320-2182c2eb` 与 `latest` 的说明，用于远程部署核对。
