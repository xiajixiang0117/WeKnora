# 手动分支构建与共享服务器部署设计

**日期：** 2026-09-03  
**状态：** 已确认，待实现

## 目标

为个人 GitHub 仓库提供一条由人工触发的交付链路。操作者在 Actions 页面输入一个分支；工作流从该分支的精确 commit 构建 WeKnora 所需镜像，推送到 GitHub Container Registry（GHCR），随后更新共享服务器 `10.21.10.145` 上运行的 WeKnora。

共享服务器不得从 Git 拉取源码或在本机编译。部署完成后，Actions 应显示实际部署的 commit 和镜像 tag，并验证服务健康状态。

## 非目标

- 不在 `push`、`pull_request`、合并 `main` 或打 tag 时自动部署。
- 不迁移仓库到 OpenSiFli，也不改变 `origin` / `upstream` 远程仓库。
- 不构建当前服务器未启用的 Sandbox 镜像。
- 不在工作流、仓库文件或日志中保存服务器密码、SSH 私钥、GHCR 令牌或 `.env` 内容。
- 不进行失败后的自动回滚；数据库自动迁移可能使旧版本不可安全恢复。

## 当前环境

- 仓库：`xiajixiang0117/WeKnora`。
- 目标机：Linux `amd64`；Docker 29.1.3、Docker Compose 2.40.3。
- 部署根目录：`/home/ubuntu/WeKnora`，由 `docker-compose.yml` 与 `docker-compose.override.yml` 共同定义服务；该目录不是 Git 工作树。
- 正在运行并应由 CI 更新的服务：`frontend`、`app`、`docreader`。PostgreSQL、Redis、数据卷和现有 `.env` 保持不变。

## 架构

```
workflow_dispatch(branch)
  -> checkout 精确 commit
  -> 构建 ui / app / docreader (linux/amd64)
  -> 推送 GHCR 的 sha-<commit> 不可变标签
  -> SSH 到共享服务器
  -> Compose 覆盖三个 image 并 pull / up -d
  -> 健康检查、部署摘要、记录成功版本
```

每次部署使用一个三个镜像共用的 tag，格式为 `sha-<12 位 commit SHA>`。镜像名称为：

- `ghcr.io/<repository-owner>/weknora-ui`
- `ghcr.io/<repository-owner>/weknora-app`
- `ghcr.io/<repository-owner>/weknora-docreader`

不发布或消费 `latest`。同一 commit 的手动重跑复用同一个不可变 tag；Docker 构建通过 GH Actions 缓存加速。

## 工作流行为

新增一个仅含 `workflow_dispatch` 触发器的 GitHub Actions 工作流。

### 输入

| 输入 | 用途 |
| --- | --- |
| `operation` | `deploy`（默认）或 `rollback`。 |
| `branch` | `deploy` 时必填的 Git 分支名称；工作流检出该分支的精确 SHA。 |
| `rollback_tag` | `rollback` 时可选；留空时使用服务器记录的前一个成功部署 tag。 |

`deploy` 依次执行：准备版本信息、构建并推送 UI、App、Docreader、部署、健康检查。构建采用现有生产构建约定：UI 先运行 `scripts/build_frontend_dist.sh`；App 使用 `docker/Dockerfile.app` 与 `WITH_ANYDOC=1`；Docreader 使用 `docker/Dockerfile.docreader`。所有目标平台固定为 `linux/amd64`，与服务器一致。

`rollback` 不构建新镜像。它读取指定 tag 或服务器保存的前一成功版本，使用相同的受控 Compose 覆盖文件重新拉取并启动三个应用服务。首次 GHCR 成功部署之前不存在可用回滚版本时，工作流应明确失败并提示原因。

## 服务器部署机制

仓库包含一个只覆盖镜像字段的 Compose 文件，例如 `deploy/docker-compose.ghcr.yml`：

```yaml
services:
  frontend:
    image: ghcr.io/${WEKNORA_IMAGE_OWNER}/weknora-ui:${WEKNORA_IMAGE_TAG}
  app:
    image: ghcr.io/${WEKNORA_IMAGE_OWNER}/weknora-app:${WEKNORA_IMAGE_TAG}
  docreader:
    image: ghcr.io/${WEKNORA_IMAGE_OWNER}/weknora-docreader:${WEKNORA_IMAGE_TAG}
```

工作流每次将此无机密的文件传至 `/home/ubuntu/WeKnora/.ci/docker-compose.ghcr.yml`。远端命令显式叠加下列三个 Compose 文件：

1. `docker-compose.yml`
2. `docker-compose.override.yml`
3. `.ci/docker-compose.ghcr.yml`

远端脚本以环境变量传入当前 image owner 与 tag，然后按顺序执行：

1. 使用只读 GHCR 令牌登录 `ghcr.io`；
2. `docker compose config -q` 验证最终配置；
3. 对 `frontend`、`app`、`docreader` 执行 `pull`；
4. 以 `up -d --no-build` 更新这三个服务，不触碰数据库、Redis、数据卷或其他 profile 服务；
5. 等待 App 与 Docreader 的 Docker healthcheck 通过，并从服务器本机请求前端 HTTP；
6. 在全部检查通过后，原子写入 `.ci/current-successful.env`，并将先前值移至 `.ci/previous-successful.env`。

若拉取、启动或健康检查失败，脚本打印受限诊断（Compose 状态与三个服务的最近日志）并以非零状态退出。它不打印环境变量、不读取 `.env`、也不自动启动回滚。上一个成功 tag 仍保存于服务器，供人工触发的 `rollback` 使用。

## 安全模型

发布作业使用 `GITHUB_TOKEN`，工作流最小权限为：

```yaml
permissions:
  contents: read
  packages: write
```

部署作业绑定 GitHub Environment `shared-server`。Environment 应保存：

| 名称 | 类型 | 用途 |
| --- | --- | --- |
| `DEPLOY_HOST` | Secret 或变量 | 服务器地址。 |
| `DEPLOY_USER` | Secret 或变量 | SSH 用户。 |
| `DEPLOY_PATH` | Secret 或变量 | Compose 根目录。 |
| `DEPLOY_KNOWN_HOSTS` | Secret | 固定的 SSH host key 行，用于防止中间人攻击。 |
| `DEPLOY_SSH_PRIVATE_KEY` | Secret | 专用 Ed25519 部署私钥。 |
| `GHCR_PULL_USERNAME` | Secret | 拥有只读 token 的 GitHub 用户名。 |
| `GHCR_PULL_TOKEN` | Secret | 仅 `read:packages` 权限的 GHCR 令牌。 |

服务器配置一个专用部署公钥到 `ubuntu` 用户的 `~/.ssh/authorized_keys`。工作流不使用 SSH 密码，也不执行 `ssh-keyscan` 后盲目信任结果；仅使用已固定的 `DEPLOY_KNOWN_HOSTS`。由于 Docker 组可获得近似主机 root 权限，部署密钥不得用于其他机器或用途。

## 可观测性与运维

每次运行的 GitHub Actions 摘要必须包含：触发者、分支、精确 commit SHA、三个 GHCR 镜像完整引用、部署状态及健康检查结果。并发策略应确保同一共享环境在任意时刻只运行一次部署，较新的手动部署不得自动取消已开始的更新。

首次部署前，操作者需要创建 `shared-server` Environment 并配置上述 Secrets；在服务器安装部署公钥；确认 GHCR Package 对该只读凭据可见。旧的密码认证不纳入自动化，应在部署密钥验证可用后轮换服务器登录密码。

## 测试与验收

1. Actions YAML 通过静态语法检查，并可从手动输入的有效分支成功检出精确 SHA。
2. UI、App、Docreader 都推送到 GHCR，且 tag 与 SHA 一致、平台为 `linux/amd64`。
3. 远端 Compose 渲染结果只改变三项应用镜像，不改变 `.env`、卷、PostgreSQL 或 Redis。
4. 部署后三个服务启动，App 与 Docreader 健康、前端 HTTP 可访问。
5. 无效分支、镜像拉取失败、健康检查失败会使工作流失败，且日志中不泄露 Secrets。
6. 成功部署后，手动 `rollback` 可用指定 tag 或前一个成功 tag 恢复三个应用服务。
