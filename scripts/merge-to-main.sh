#!/usr/bin/env bash
# 将 agent-go 分支合并到 main 并推送，最后切回 agent-go
set -euo pipefail

# 切换到仓库根目录，避免切换分支后当前目录不存在导致报错
cd "$(git rev-parse --show-toplevel)"

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

# 确保从 agent-go 分支开始
if [ "$CURRENT_BRANCH" != "agent-go" ]; then
  echo "当前不在 agent-go 分支，当前分支: $CURRENT_BRANCH"
  echo "请先切换到 agent-go 分支再运行此脚本"
  exit 1
fi

echo ">>> 切换到 main 分支"
git checkout main

echo ">>> 合并 agent-go 到 main"
git merge agent-go

echo ">>> 推送 main 到远程"
git push origin main

echo ">>> 切回 agent-go 分支"
git checkout agent-go

echo ">>> 完成！"
