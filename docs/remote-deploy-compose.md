# 远程单机部署步骤文档

本文档只覆盖一种场景：

- 已经把 `new-api` 镜像推送到远程仓库
- 远程服务器是单机部署
- 使用 `docker compose` 启动 `new-api`、`postgres`、`redis`

本文档不讨论多机主从，也不使用 `.env` 文件。
数据库密码、应用连接串这类运行参数，直接写在
`docker-compose.yml` 里。

## 1. 前置条件

远程服务器需要满足：

- 已安装 Docker
- 已安装 `docker compose`
- 可以访问 Docker Hub
- 服务器磁盘空间足够

先检查：

```bash
docker --version
docker compose version
docker info
```

## 2. 创建部署目录

建议在服务器上使用固定目录：

```text
/home/new-api/
├── docker-compose.yml
├── data/
├── postgres/
└── logs/
```

创建目录：

```bash
sudo mkdir -p /home/new-api/data
sudo mkdir -p /home/new-api/postgres
sudo mkdir -p /home/new-api/logs
cd /home/new-api
```

## 3. 准备 docker-compose.yml

在服务器上创建 `docker-compose.yml`：

```yaml
services:
  new-api:
    image: akon/new-api:20260317-0eee3411
    container_name: new-api
    restart: always
    command: --log-dir /app/logs
    ports:
      - "3000:3000"
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    environment:
      - SQL_DSN=postgresql://newapi:your_postgres_password@postgres:5432/newapi
      - REDIS_CONN_STRING=redis://redis:6379/0
      - TZ=Asia/Shanghai
      - GIN_MODE=release
      - SESSION_SECRET=your_session_secret
      - CRYPTO_SECRET=your_crypto_secret
      - ERROR_LOG_ENABLED=true
      - BATCH_UPDATE_ENABLED=true
    depends_on:
      - redis
      - postgres
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O - http://localhost:3000/api/status | grep -o '\"success\":\\s*true' || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3

  redis:
    image: redis:7-alpine
    container_name: redis
    restart: always

  postgres:
    image: postgres:15
    container_name: postgres
    restart: always
    environment:
      POSTGRES_USER: newapi
      POSTGRES_PASSWORD: your_postgres_password
      POSTGRES_DB: newapi
    volumes:
      - ./postgres:/var/lib/postgresql/data
```

## 4. 如果你基于仓库自带 docker-compose.yml 修改

如果你直接拿仓库根目录的 `docker-compose.yml` 去远程部署，
不需要整份重写，只改下面几个必须修改的位置：

1. 镜像地址  
   把 `new-api` 服务里的 `image` 改成你推送的标签。  
   当前仓库位置：`docker-compose.yml:19`

2. 应用密钥  
   在 `new-api` 的 `environment` 里新增 `SESSION_SECRET` 和
   `CRYPTO_SECRET`。  
   当前仓库位置：`docker-compose.yml:28`

3. PostgreSQL 数据目录  
   把 `postgres` 服务的 `volumes` 改成映射到
   本地目录，方便后续做迁移和备份。  
   数据目录映射位置：`docker-compose.yml:67`

## 5. 这几个参数为什么必须写

下面这几项不是 Docker Hub 登录信息，而是容器运行配置：

- `POSTGRES_USER`
  作用是初始化 PostgreSQL 用户
- `POSTGRES_PASSWORD`
  作用是初始化 PostgreSQL 用户密码
- `POSTGRES_DB`
  作用是初始化默认数据库
- `SQL_DSN`
  作用是让 `new-api` 连接上面这个 PostgreSQL
- `SESSION_SECRET`
  作用是应用会话密钥
- `CRYPTO_SECRET`
  作用是应用加密密钥

其中最关键的一点是：

`SQL_DSN` 里的用户名和密码，必须和 `postgres` 服务里的
`POSTGRES_USER`、`POSTGRES_PASSWORD` 对应上。

例如上面的示例里：

```text
POSTGRES_USER=newapi
POSTGRES_PASSWORD=your_postgres_password
SQL_DSN=postgresql://newapi:your_postgres_password@postgres:5432/newapi
```

这三处必须保持一致。

## 6. Docker Hub 登录

如果你的镜像仓库是私有的，先在服务器手动登录：

```bash
docker login
```

如果镜像仓库是公开的，这一步可以跳过。

这里的 `docker login` 只负责拉镜像认证，
和 `POSTGRES_PASSWORD`、`SQL_DSN` 这些运行参数没有关系。

## 7. 启动服务

进入部署目录后执行：

```bash
cd /home/new-api
docker compose pull
docker compose up -d
docker compose ps
```

如果你的服务器只有旧命令，也可以写成：

```bash
docker-compose pull
docker-compose up -d
docker-compose ps
```

## 8. 验证部署结果

查看容器状态：

```bash
docker compose ps
```

查看应用日志：

```bash
docker compose logs -f new-api
```

检查健康接口：

```bash
curl http://127.0.0.1:3000/api/status
```

如果服务器直接暴露了 3000 端口，也可以浏览器访问：

```text
http://<服务器IP>:3000
```

## 9. 常见问题

### 9.1 new-api 启动失败

先看日志：

```bash
docker compose logs --tail=200 new-api
docker compose logs --tail=200 postgres
docker compose logs --tail=200 redis
```

重点检查：

- `SQL_DSN` 用户名和密码是否与 PostgreSQL 初始化参数一致
- `SESSION_SECRET` 是否还写成了默认值
- 镜像标签是否存在
- 端口 `3000` 是否被占用

### 9.2 拉镜像失败

重点检查：

- 镜像名是否正确
- 标签是否存在
- 私有仓库是否已经执行 `docker login`
- 服务器是否能访问 Docker Hub

### 9.3 PostgreSQL 改密码后不生效

这是 PostgreSQL 官方镜像的正常行为。

`POSTGRES_PASSWORD` 主要在首次初始化数据目录时生效。
如果 `./postgres` 已经存在，后面你再改
`docker-compose.yml` 里的密码，不会自动把旧数据库密码改掉。

这种情况要么：

- 用数据库内部命令手动改密码
- 要么清理旧数据后重新初始化数据库

## 10. 后续升级

每次发布新镜像后，单机服务器只要做这几步：

1. 修改 `docker-compose.yml` 里的镜像标签
2. 执行 `docker compose pull`
3. 执行 `docker compose up -d`
4. 用日志和状态确认升级完成

例如把镜像改成新版本后执行：

```bash
docker compose pull
docker compose up -d
docker compose ps
```

## 11. 回滚

如果新版本有问题，就把 `docker-compose.yml` 里的镜像标签
改回上一个可用版本，然后执行：

```bash
docker compose pull
docker compose up -d
```

## 12. 推荐上线顺序

建议按下面顺序操作：

1. 本地构建并推送镜像
2. 登录远程服务器
3. 手动执行 `docker login`（仅私有仓库需要）
4. 编辑 `docker-compose.yml`
5. 执行 `docker compose pull`
6. 执行 `docker compose up -d`
7. 查看 `docker compose ps`
8. 查看 `docker compose logs -f new-api`

这样最直接，也最容易排错。
