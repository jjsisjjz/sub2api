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
