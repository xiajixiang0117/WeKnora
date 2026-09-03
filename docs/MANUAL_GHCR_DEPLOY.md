# 手动部署到共享服务器

[`Manual Build and Deploy`](../.github/workflows/manual-ghcr-deploy.yml) 不会在任何 Git 事件上自动运行。请在 GitHub Actions 页面手动触发它：选择 `deploy`，填写要构建的分支；工作流会构建 UI、App、Docreader 三个 `linux/amd64` 镜像，推送到 GHCR，并部署到共享服务器。

部署镜像采用 `sha-<commit>` 不可变 tag。首次成功部署后，可选择 `rollback`；留空 `rollback_tag` 会使用服务器记录的前一个成功版本，填写 tag 则部署指定的历史镜像。

## 一次性配置

在仓库 Settings → Environments 创建 `shared-server`，并配置以下 Environment Secrets：

| Secret | 值 |
| --- | --- |
| `DEPLOY_HOST` | 服务器地址。 |
| `DEPLOY_USER` | 部署用 SSH 用户。 |
| `DEPLOY_PATH` | Compose 根目录，例如 `/home/ubuntu/WeKnora`。 |
| `DEPLOY_KNOWN_HOSTS` | 目标服务器的固定 OpenSSH known-hosts 行。 |
| `DEPLOY_SSH_PRIVATE_KEY` | 专用 Ed25519 部署私钥。 |
| `GHCR_PULL_USERNAME` | 只读 GHCR 令牌所属的 GitHub 用户名。 |
| `GHCR_PULL_TOKEN` | 只有 `read:packages` 权限的 GitHub token。 |

将对应部署公钥加入服务器 `DEPLOY_USER` 的 `~/.ssh/authorized_keys`。私钥不得提交进仓库或存放在服务器。`DEPLOY_KNOWN_HOSTS` 必须来自经核验的服务器 host key；工作流不会使用 `ssh-keyscan` 动态信任目标机器。

GitHub Actions 会用内置的 `GITHUB_TOKEN` 发布镜像。首次发布后，确认三个 GHCR Package 对 `GHCR_PULL_TOKEN` 可见；如果仓库或 Package 为私有，这个 token 仍须具有 `read:packages`。

## 服务器行为

工作流仅写入 `<DEPLOY_PATH>/.ci/` 下的无机密 Compose 覆盖和成功版本记录。实际更新命令始终叠加：

1. `docker-compose.yml`
2. `docker-compose.override.yml`
3. `.ci/docker-compose.ghcr.yml`

因此 `.env`、数据卷、PostgreSQL、Redis 与其他 Compose profile 不会被 CI 改动。部署会先拉取所有三个镜像；再依次更新 Docreader、App、Frontend，并确认后端健康检查和本机前端 HTTP 可用。

GHCR 拉取凭据只在本次远端命令的临时 Docker 配置目录中使用，结束时会清除；不会覆盖服务器用户已有的 Docker 登录状态。

失败时工作流保留故障现场并输出三个应用服务的受限诊断，不自动回滚。App 默认会运行数据库迁移，自动切回旧二进制可能与已迁移的数据不兼容。
