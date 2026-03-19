# CentOS Linux 7 下使用本地编译产物接入 Docker Compose 的部署流程

## 1. 适用范围

本文档只针对：

- 操作系统是 `CentOS Linux 7`
- 代码在服务器本机拉取
- 服务器已经安装好 `Docker` 和 `Compose`
- 先在服务器本机完成构建
- 再交给 `Docker Compose` 编排运行

## 2. 先说结论

这个项目不能只执行一次 `go build`。

正确顺序必须是：

```text
先构建前端 web/dist
再编译后端二进制
再把本地编译好的二进制打进本地镜像
最后用 docker compose 启动
```

原因是后端会在编译时嵌入前端静态资源。

## 3. CentOS 7 的边界要先讲清楚

`CentOS Linux 7` 已于 `2024-06-30` 结束生命周期。

截至 `2026-03-17`：

- Docker 官方安装文档主线只维护 `CentOS Stream 9` 和 `CentOS Stream 10`
- 你现在这个场景属于“遗留系统继续部署”

所以这份文档的目标不是把 `CentOS 7` 包装成推荐环境，而是给你一套在现有遗留机器上还能落地的流程。

## 4. 部署思路

为了减少 CentOS 7 上的兼容问题，本文采用下面这套方式：

1. 宿主机已经具备可用的 `Docker` 和 `Compose`
2. 前端不要求在宿主机直接安装 `Bun`
3. 宿主机需要安装 `Go` 和 `Git`
4. 前端构建改用 `oven/bun` 容器完成
5. 后端仍在宿主机本地执行 `go build`
6. `docker compose` 只负责运行你本地编译后的程序

这样更稳，尤其适合 CentOS 7。

## 5. 准备基础环境

### 5.1 安装常用工具

```bash
sudo yum install -y git curl wget tar yum-utils
```

### 5.2 安装 Go

到官方页面下载 Linux AMD64 压缩包后执行：

```bash
cd /usr/local/src
sudo tar -C /usr/local -xzf go<版本号>.linux-amd64.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
go version
```

如果系统里已经有旧版 Go，先清理：

```bash
sudo rm -rf /usr/local/go
```

## 6. 拉代码并准备目录

下面示例统一用 `/opt/new-api`：

```bash
sudo mkdir -p /opt/new-api
sudo chown -R $USER:$USER /opt/new-api
cd /opt/new-api
git clone <你的仓库地址> src
cd src
```

再准备部署目录：

```bash
mkdir -p /opt/new-api/deploy/bin
mkdir -p /opt/new-api/deploy/data
mkdir -p /opt/new-api/deploy/logs
```

推荐目录结构：

```text
/opt/new-api/
├── src/
└── deploy/
    ├── bin/
    ├── data/
    ├── logs/
    ├── .env
    ├── Dockerfile.local
    └── docker-compose.local.yml
```

## 7. 构建前端

CentOS 7 不建议强依赖宿主机安装 Bun。

这里直接用官方 `oven/bun` 镜像构建前端：

```bash
cd /opt/new-api/src
docker run --rm \
  -u $(id -u):$(id -g) \
  -v /opt/new-api/src:/work \
  -w /work/web \
  oven/bun:latest \
  sh -lc "bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../VERSION) bun run build"
```

构建成功后，前端产物会落在：

```text
/opt/new-api/src/web/dist
```

## 8. 编译后端

前端构建完成后，再执行后端编译：

```bash
cd /opt/new-api/src
mkdir -p build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build \
  -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" \
  -o build/new-api
chmod +x build/new-api
```

然后复制到部署目录：

```bash
cp /opt/new-api/src/build/new-api /opt/new-api/deploy/bin/new-api
cp /opt/new-api/src/.env.example /opt/new-api/deploy/.env
```

## 9. 准备本地运行镜像

在 `/opt/new-api/deploy` 下创建 `Dockerfile.local`：

```dockerfile
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY bin/new-api /new-api
RUN chmod +x /new-api

EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
```

这一步的重点是：

- 不复制源码
- 不在镜像里重新编译
- 只复制你刚才本地编出来的 `new-api`

## 10. 编写 Compose 编排文件

在 `/opt/new-api/deploy` 下创建 `docker-compose.local.yml`：

```yaml
services:
  new-api:
    build:
      context: .
      dockerfile: Dockerfile.local
    image: new-api-local:latest
    container_name: new-api
    restart: always
    command: --log-dir /app/logs
    ports:
      - "3000:3000"
    env_file:
      - .env
    environment:
      TZ: Asia/Shanghai
      SQL_DSN: postgresql://root:123456@postgres:5432/new-api
      REDIS_CONN_STRING: redis://redis:6379/0
      ERROR_LOG_ENABLED: "true"
      BATCH_UPDATE_ENABLED: "true"
    volumes:
      - ./data:/data:Z
      - ./logs:/app/logs:Z
    depends_on:
      - redis
      - postgres
    networks:
      - new-api-network
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O - http://localhost:3000/api/status | grep -o '\"success\":\\s*true' || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3

  redis:
    image: redis:latest
    container_name: redis
    restart: always
    networks:
      - new-api-network

  postgres:
    image: postgres:15
    container_name: postgres
    restart: always
    environment:
      POSTGRES_USER: root
      POSTGRES_PASSWORD: 123456
      POSTGRES_DB: new-api
    volumes:
      - pg_data:/var/lib/postgresql/data
    networks:
      - new-api-network

volumes:
  pg_data:

networks:
  new-api-network:
    driver: bridge
```

CentOS 7 常见启用 `SELinux`，所以挂载卷保留 `:Z`。

## 11. 修改环境变量

进入部署目录：

```bash
cd /opt/new-api/deploy
vim .env
```

至少检查这些项目：

- `SESSION_SECRET`
- `SQL_DSN`
- `REDIS_CONN_STRING`
- 管理员初始化相关变量
- 第三方模型渠道变量
- 邮件和 OAuth 变量

## 12. 启动服务

如果你的机器支持 `docker compose`：

```bash
cd /opt/new-api/deploy
docker compose -f docker-compose.local.yml build --no-cache
docker compose -f docker-compose.local.yml up -d
docker compose -f docker-compose.local.yml ps
```

如果你的机器只有 `docker-compose`：

```bash
cd /opt/new-api/deploy
docker-compose -f docker-compose.local.yml build --no-cache
docker-compose -f docker-compose.local.yml up -d
docker-compose -f docker-compose.local.yml ps
```

查看日志：

```bash
docker compose -f docker-compose.local.yml logs -f new-api
```

如果你只有 `docker-compose`，这里同样替换命令即可。

## 13. 验证是否正常

### 13.1 查看容器状态

```bash
docker compose -f docker-compose.local.yml ps
```

### 13.2 查看健康检查

```bash
docker inspect --format='{{json .State.Health}}' new-api
```

### 13.3 检查接口

```bash
curl http://127.0.0.1:3000/api/status
```

## 14. 后续更新流程

以后每次改代码，按这个顺序更新：

```bash
cd /opt/new-api/src
git pull

docker run --rm \
  -u $(id -u):$(id -g) \
  -v /opt/new-api/src:/work \
  -w /work/web \
  oven/bun:latest \
  sh -lc "bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../VERSION) bun run build"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build \
  -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" \
  -o build/new-api

cp build/new-api /opt/new-api/deploy/bin/new-api

cd /opt/new-api/deploy
docker compose -f docker-compose.local.yml build new-api
docker compose -f docker-compose.local.yml up -d new-api
```

如果你的服务器只有 `docker-compose`，把最后两条命令替换掉即可。

## 15. 常见问题

### 15.1 页面空白

多数情况是你只编译了 Go，没有先生成 `web/dist`。

### 15.2 Compose 命令不可用

这不是项目问题，通常是：

- Docker Compose 插件没装上
- 机器上只有 `docker-compose`
- CentOS 7 的 Docker 版本过旧

### 15.3 数据库连不上

重点检查：

- `SQL_DSN` 里主机名是不是 `postgres`
- 密码是不是和编排文件一致
- 不要把容器内数据库地址写成 `localhost`

### 15.4 SELinux 导致挂载目录权限问题

保留这两个卷后缀：

- `./data:/data:Z`
- `./logs:/app/logs:Z`

## 16. 最短执行清单

如果你只看最短步骤，就按这个顺序：

1. 在 CentOS 7 上准备好 Docker、Compose、Go、Git
2. 确认宿主机已有可用的 Docker、Compose、Go、Git
3. 把源码拉到 `/opt/new-api/src`
4. 用 `oven/bun` 容器构建 `web/dist`
5. 用宿主机 `go build` 生成 `build/new-api`
6. 把二进制复制到 `/opt/new-api/deploy/bin/new-api`
7. 写 `Dockerfile.local` 和 `docker-compose.local.yml`
8. 执行 `docker compose build`
9. 执行 `docker compose up -d`
10. 用 `curl http://127.0.0.1:3000/api/status` 验证

## 17. 官方参考

- Go 安装文档：`https://go.dev/doc/install`
- CentOS 7 EOL 公告：`https://www.centos.org/centos-linux-eol/`

## 18. 总结

这份文档现在是按 `CentOS Linux 7` 写的，不是按 `CentOS Stream 9/10`。

核心做法只有一句话：

```text
前端先构建，后端再编译，最后让 Docker Compose 运行本地编译产物
```
