# 手动从源码部署到共享服务器

[`Manual Source Build and Deploy`](../.github/workflows/manual-source-deploy.yml) 不会在 Git 事件上自动运行。请在 GitHub Actions 页面手动触发它，填写要拉取的 Git 分支和本次镜像版本号（例如 `v1.3.0`）。工作流在共享服务器上执行部署脚本；脚本从 GitHub 拉取该分支源码，在服务器本机构建 UI、App、Docreader 镜像，再更新服务。

也可以直接登录服务器执行：

```sh
cd /home/ubuntu/WeKnora
bash deploy/source-compose-deploy.sh main v1.3.0
```

版本号必须是合法的 Docker image tag，会同时用于 `wechatopenai/weknora-ui`、`wechatopenai/weknora-app` 与 `wechatopenai/weknora-docreader` 的本地镜像标签。脚本会将实际分支、版本、commit 和部署时间记录到 `.ci/current-source-deployment.env`。

## 一次性配置

部署目录必须是可以拉取 GitHub 源码的 Git 工作树，且部署用户应能运行 Docker。前端由部署脚本在 `node:24-bookworm-slim` 容器内构建，因此服务器不需要安装 Node.js 或 npm；首次部署会拉取该构建镜像：

```sh
git clone git@github.com:xiajixiang0117/WeKnora.git /home/ubuntu/WeKnora
cd /home/ubuntu/WeKnora
chmod +x deploy/source-compose-deploy.sh
```

私有仓库需要为该服务器的部署用户配置只读 GitHub Deploy Key，或以其他只读凭据配置 `origin`；凭据不得写入仓库文件。将生产用 `.env`、`docker-compose.override.yml` 和需要保留的数据卷配置在该目录中，但不要提交其中的机密信息。

如果现有部署目录不是 Git 工作树，不要在其上强行执行脚本。先备份 `.env`、Compose 覆盖文件和部署配置，再迁移至新的 Git 工作树或由运维人员将现有目录安全地初始化为该仓库。

在 GitHub 的 `shared-server` Environment 中只需配置：

| Secret | 值 |
| --- | --- |
| `DEPLOY_PATH` | 服务器上的 Git 工作树路径，例如 `/home/ubuntu/WeKnora`。 |

现有 self-hosted runner 必须运行在这台服务器，并使用能访问该路径、GitHub `origin` 和 Docker daemon 的账户。此模式不需要 GHCR 推送或拉取凭据，也不需要 SSH 私钥。

## 部署行为

脚本会先以非快进拒绝的方式更新请求分支（`git pull --ff-only origin <branch>`），因此不会意外合并服务器上的本地提交。随后它会：

1. 为前端生成静态资源，并从本机源码构建 `frontend`、`app`、`docreader`；
2. 先更新 Docreader 与 App，等待两个服务的 Docker healthcheck 通过；
3. 再更新 Frontend，并从服务器本机确认 HTTP 可访问；
4. 将本次成功部署写入 `.ci/current-source-deployment.env`。

部署锁可防止手动执行和 GitHub Actions 同时构建。PostgreSQL、Redis、数据卷和其他 Compose profile 不会被部署脚本更新。失败时会输出三个应用服务的 Compose 状态与最近日志，并保留现场；不自动回滚。
