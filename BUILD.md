# new-api 本地构建与发布说明

## 概述

这份文档基于当前仓库的实际构建结果整理，目标是解决以下几个常见问题：

- Docker `buildx` 多阶段构建过程中，前端 `vite build` 阶段偶发 `rpc EOF`
- Go 依赖下载阶段访问 `proxy.golang.org` 不稳定
- 每次发布都要手工敲很多命令

当前推荐方案不是直接依赖原始多阶段 `Dockerfile` 一把构建，而是使用仓库内的分步构建脚本：

- [build_and_push_image.ps1](/D:/github/QuantumNous/new-api/build_and_push_image.ps1)
- [Dockerfile.runtime](/D:/github/QuantumNous/new-api/Dockerfile.runtime)

这套流程会先单独构建前端产物和后端二进制，再打运行镜像，稳定性更高。

---

## 前置条件

- 已安装并启动 Docker Desktop
- 宿主机已安装 Go，且 `go version` 可用
- 可访问宿主机代理端口 `7897`
- 已登录 Docker Hub：`docker login`

说明：

- 当前脚本不要求宿主机安装 Bun
- 前端构建会在 Bun 容器中完成

---

## 推荐构建方式

在仓库根目录执行：

```powershell
Set-Location D:\github\QuantumNous\new-api
powershell -ExecutionPolicy Bypass -File .\build_and_push_image.ps1
```

默认会构建本地镜像：

```text
akon/new-api:20260319-32ece0ee
```

如果你要指定其他 tag：

```powershell
Set-Location D:\github\QuantumNous\new-api
powershell -ExecutionPolicy Bypass -File .\build_and_push_image.ps1 -Tag 20260319-mytag
```

如果你要构建后直接推送：

```powershell
Set-Location D:\github\QuantumNous\new-api
powershell -ExecutionPolicy Bypass -File .\build_and_push_image.ps1 -Push
```

---

## 脚本实际做了什么

`build_and_push_image.ps1` 的流程如下：

1. 使用 Bun 容器构建 `web/dist`
2. 使用宿主机 Go 交叉编译 `linux/amd64` 二进制到 `out/new-api`
3. 使用 [Dockerfile.runtime](/D:/github/QuantumNous/new-api/Dockerfile.runtime) 打包最终运行镜像
4. 如果带 `-Push`，则推送镜像到 Docker Hub

这样做的原因：

- 绕开 `buildx` 多阶段里最容易不稳定的前端构建阶段
- Go 下载依赖时显式使用 `GOPROXY=https://goproxy.cn,direct`
- 构建失败时可以明确定位是前端、Go 编译，还是镜像打包

---

## 代理配置

脚本默认使用以下代理：

- 容器内 HTTP/HTTPS：`http://host.docker.internal:7897`
- 容器内 SOCKS：`socks5://host.docker.internal:7897`
- 宿主机 Go：`http://127.0.0.1:7897`

如果你的代理地址不同，可以这样覆盖：

```powershell
Set-Location D:\github\QuantumNous\new-api
powershell -ExecutionPolicy Bypass -File .\build_and_push_image.ps1 `
  -ContainerProxy http://host.docker.internal:7897 `
  -ContainerAllProxy socks5://host.docker.internal:7897 `
  -HostProxy http://127.0.0.1:7897 `
  -HostAllProxy socks5://127.0.0.1:7897 `
  -GoProxy https://goproxy.cn,direct
```

---

## 镜像检查

构建完成后检查本地镜像：

```powershell
docker images akon/new-api
```

如果只想检查某个 tag：

```powershell
docker images akon/new-api:20260319-32ece0ee
```

---

## 发布建议

建议同时保留“版本 tag”和 `latest`。

例如先推版本 tag：

```powershell
docker push akon/new-api:20260319-32ece0ee
```

再更新 `latest`：

```powershell
docker tag akon/new-api:20260319-32ece0ee akon/new-api:latest
docker push akon/new-api:latest
```

这样做的好处：

- 线上部署可以固定写 `latest`
- 出问题时仍然可以回滚到某个历史版本 tag

---

## 远程 Compose 更新

远程 `docker-compose.yml` 建议写成：

```yaml
services:
  new-api:
    image: akon/new-api:latest
```

发布新镜像后，在远程服务器部署目录执行：

```bash
docker compose pull new-api
docker compose up -d --force-recreate new-api
```

如果是老版 Compose：

```bash
docker-compose pull new-api
docker-compose up -d --force-recreate new-api
```

检查当前运行镜像：

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

如果你看到日志停在：

```text
transforming...
Browserslist: browsers data (caniuse-lite) is 10 months old
```

真正的问题往往是后续的：

- Docker Desktop / BuildKit `rpc EOF`
- 网络抖动导致 tarball 或依赖下载失败

如果你只想更新 Browserslist 数据，可以在 `web` 目录执行：

```powershell
npx update-browserslist-db@latest
```

或：

```powershell
bunx update-browserslist-db@latest
```

---

### 2. `go mod download` 访问 `proxy.golang.org` 失败

当前推荐直接使用：

```text
GOPROXY=https://goproxy.cn,direct
```

脚本已经内置了这个默认值。

---

### 3. `bun install` 报 tarball integrity check failed

这是网络抖动或缓存损坏导致的常见问题。当前脚本已经做了两件事：

- 使用临时 Docker volume 存放 `node_modules`
- 前端构建失败时自动重试一次

如果仍然失败，优先检查：

- 代理是否可用
- Docker Desktop CPU / 内存是否被打满

---

### 4. Docker API 返回 500

例如：

```text
request returned 500 Internal Server Error for API route ... /docker_engine/_ping
```

通常说明 Docker Desktop 没完全起来，或者 Docker Engine 短暂异常。

先执行：

```powershell
docker version
docker buildx ls
```

确认 `desktop-linux` 或 `default` builder 处于 `running` 状态后再构建。

---

## 相关文件

- 构建脚本：[build_and_push_image.ps1](/D:/github/QuantumNous/new-api/build_and_push_image.ps1)
- 运行镜像 Dockerfile：[Dockerfile.runtime](/D:/github/QuantumNous/new-api/Dockerfile.runtime)
- 原始多阶段 Dockerfile：[Dockerfile](/D:/github/QuantumNous/new-api/Dockerfile)
