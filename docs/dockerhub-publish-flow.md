# Docker Hub 创建仓库并发布镜像流程

## 1. 目标

这份文档用于以下场景：

- 你已经在本地完成镜像构建
- 你想把镜像推送到 `Docker Hub`
- 服务器后续只负责 `pull` 镜像并用 `docker compose` 启动

## 2. 先准备这几个信息

开始之前，先确认：

- 你的 `Docker Hub` 用户名
- 你准备使用的仓库名
- 你准备使用的镜像标签

示例：

```text
用户名: yourname
仓库名: new-api
标签: 2026-03-17
完整镜像名: docker.io/yourname/new-api:2026-03-17
```

## 3. 创建 Docker Hub 账号

如果你还没有账号：

1. 打开 `https://app.docker.com/signup`
2. 按页面提示注册账号
3. 完成邮箱验证
4. 登录 `Docker Hub`

## 4. 创建公有仓库

登录后按下面步骤创建：

1. 进入 `My Hub`
2. 打开 `Repositories`
3. 点击右上角 `Create repository`
4. 选择你的 `Namespace`
5. 填写 `Repository Name`
6. `Visibility` 选择 `Public`
7. 点击 `Create`

创建完成后，仓库地址通常是：

```text
docker.io/<你的用户名>/<仓库名>
```

示例：

```text
docker.io/yourname/new-api
```

## 5. 创建 Personal Access Token

建议不要直接用网页登录密码做 CLI 推送。

推荐步骤：

1. 点击右上角头像
2. 进入 `Account settings`
3. 打开 `Personal access tokens`
4. 点击 `Generate new token`
5. 名称随意，例如：

```text
new-api-push
```

6. 权限选择包含 `Read` 和 `Write`
7. 创建后立刻复制保存

注意：

- 这个 token 通常只显示一次
- 丢了就重新生成

## 6. 本地登录 Docker Hub

在本地终端执行：

```bash
docker login --username <你的用户名>
```

然后在密码输入位置粘贴刚才生成的 `Personal Access Token`。

登录成功后通常会看到：

```text
Login Succeeded
```

## 7. 给本地镜像重新打 tag

假设你本地已有镜像：

```text
new-api:local-20260317-0eee3411
```

那么重新打 tag：

```bash
docker tag new-api:local-20260317-0eee3411 docker.io/<你的用户名>/new-api:2026-03-17
```

示例：

```bash
docker tag new-api:local-20260317-0eee3411 docker.io/yourname/new-api:2026-03-17
```

如果你本地镜像标签不是这个名字，就把前半段替换成你真实的本地 tag。

## 8. 推送到 Docker Hub

执行：

```bash
docker push docker.io/<你的用户名>/new-api:2026-03-17
```

示例：

```bash
docker push docker.io/yourname/new-api:2026-03-17
```

推送成功后，你可以在 Docker Hub 仓库页面看到对应 tag。

## 9. 服务器上的 compose 写法

服务器不再需要源码构建。

`docker-compose.yml` 或 `compose.yml` 里直接写：

```yaml
services:
  new-api:
    image: docker.io/<你的用户名>/new-api:2026-03-17
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

  redis:
    image: redis:latest
    container_name: redis
    restart: always

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

volumes:
  pg_data:
```

## 10. 服务器部署命令

服务器执行：

```bash
docker compose pull
docker compose up -d
docker compose ps
```

如果你的服务器只有旧版命令，就改成：

```bash
docker-compose pull
docker-compose up -d
docker-compose ps
```

## 11. 后续更新流程

以后每次发版，只需要重复下面几步：

1. 本地重新构建镜像
2. 重新打一个新 tag
3. 推送到 Docker Hub
4. 服务器执行 `docker compose pull`
5. 服务器执行 `docker compose up -d`

推荐不要长期只用 `latest`，而是每次发版都打明确标签，例如：

```text
2026-03-17
v1.0.3
build-20260317-1
```

## 12. 常见问题

### 12.1 推送时报 `denied`

通常是以下原因：

- 仓库名写错
- 用户名写错
- 没登录成功
- 用的是账号密码，不是 `PAT`
- 当前账号对这个仓库没有写权限

### 12.2 服务器拉不到镜像

重点检查：

- 镜像名是否完整
- tag 是否存在
- 服务器网络是否能访问 `Docker Hub`

### 12.3 服务器还是旧版本

通常是因为：

- `compose` 里还写着旧 tag
- 只执行了 `up -d`，没先 `pull`
- 镜像缓存没更新

可以执行：

```bash
docker compose pull
docker compose up -d
```

## 13. 最短步骤

如果你只看最短流程，就按这个顺序：

1. 在 Docker Hub 创建 `Public Repository`
2. 创建 `Personal Access Token`
3. 本地执行 `docker login`
4. 本地执行 `docker tag`
5. 本地执行 `docker push`
6. 服务器的 `compose` 改成 `image: docker.io/<用户名>/<仓库名>:<tag>`
7. 服务器执行 `docker compose pull && docker compose up -d`

## 14. 官方参考

- Docker Hub 仓库创建文档：`https://docs.docker.com/docker-hub/repos/create/`
- Docker Access Tokens 文档：`https://docs.docker.com/security/access-tokens/`
- Docker Hub 限流说明：`https://docs.docker.com/docker-hub/download-rate-limit/`
