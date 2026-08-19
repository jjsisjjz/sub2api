# 同步上游并保留魔改

魔改固定保存在 `magic` 分支。官方仓库作为 `upstream`，你自己的 GitHub fork 作为 `origin`。以后不要再用新版 ZIP 覆盖魔改目录。

## 一次性设置

1. 在 GitHub fork `Wei-Shaw/sub2api`。
2. 克隆自己的 fork，并添加官方远程：

```powershell
$GitHubUser = Read-Host 'GitHub 用户名'
git clone "https://github.com/$GitHubUser/sub2api.git" sub2api-magic
Set-Location .\sub2api-magic
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
git switch -c magic
```

3. 将当前魔改提交到 `magic` 分支，然后推送：

```powershell
git add --all
git commit -m "feat(magic): preserve Codex quota overdraft"
git push -u origin magic
```

4. 在 GitHub 仓库的 `Settings > Actions > General` 中，将 Workflow permissions 设置为 `Read and write permissions`。

## 自动同步和下载

`.github/workflows/sync-magic.yml` 每天检查官方最新 `v*` tag，也支持在 Actions 页面手动运行。

自动流程：

1. 合并官方最新 tag 到 `magic`。
2. 运行前端测试、ESLint、生产构建和后端测试。
3. 构建内嵌前端的 Windows amd64 EXE。
4. 推送更新后的 `magic` 分支。
5. 发布 `magic-vX.Y.Z` Release，ZIP 可直接下载。

发生源码冲突时，Action 会上传 `magic-sync-conflicts-*` 文件清单并停止，不会覆盖魔改。

首次发布当前魔改基线时，在 Actions 的 `Run workflow` 中填写：

- `upstream_tag`: `v0.1.178`
- `publish`: `true`
- `rebuild_current`: `true`

`rebuild_current` 只负责重新构建当前 `magic` 分支，不会重复合并已经包含的上游 tag。

推送 `magic` 分支也会自动触发测试、构建和发布。魔改 Release 使用独立递增的三段版本号：当上游版本没有高于现有魔改版本时，自动将现有魔改版本的 patch 加一。因此同一上游版本上的修订也能被面板识别为更新，不会覆盖同版本 Release。

## 私有仓库更新

仓库设为 Private 后，源码和 Release 资产都会变为不可见。运行中的 Sub2API 必须设置 `UPDATE_GITHUB_TOKEN` 才能查询和下载私有 Release：

```powershell
$env:UPDATE_GITHUB_TOKEN = 'github_pat_xxx'
.\run.ps1
```

使用 fine-grained personal access token，只授予本仓库只读 `Contents` 权限。Token 仅发送到 `api.github.com`，跳转到 GitHub 资产存储前会移除认证头；不要把 Token 提交到仓库或打进发布包。

从旧版切换到私有更新时必须按此顺序操作：

1. 保持仓库公开，发布并安装首个包含私有下载支持的 bootstrap 版本。
2. 在部署环境设置 `UPDATE_GITHUB_TOKEN` 并重启服务。
3. 将 GitHub 仓库切换为 Private。
4. 在版本面板强制刷新，确认仍能读取最新 Release。

## 本地同步

在干净的 `magic` 分支运行：

```powershell
.\tools\sync-magic.ps1
```

确认结果后推送：

```powershell
.\tools\sync-magic.ps1 -Push
```

指定官方版本：

```powershell
.\tools\sync-magic.ps1 -UpstreamTag v0.1.178 -Push
```

如果合并发生冲突：

```powershell
git status
# 编辑冲突文件并删除冲突标记
git add <resolved-files>
corepack pnpm --dir frontend test:run
go test -C backend ./...
git commit
git push origin magic
```

运行数据始终放在独立的 `local-runtime`，同步源码和发布包时不要覆盖 `config.yaml`、PostgreSQL 或 Redis 数据目录。
