#!/usr/bin/env bash
# 打包 macOS ARM64 (Apple Silicon) 安装包
# 产物: release/darwin-arm64/AgentForge-<version>-darwin-arm64.zip
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

# 从 wails.json 读取版本号
VERSION=$(grep -o '"productVersion": *"[^"]*"' wails.json | head -1 | sed 's/.*": *"//;s/"//')
APP_NAME="agent-rust"
RELEASE_DIR="$ROOT_DIR/release/darwin-arm64"

echo "=========================================="
echo "  AgentForge macOS ARM64 打包"
echo "  版本: $VERSION"
echo "=========================================="

# 1. 前置检查
echo ""
echo "[1/5] 检查依赖..."
command -v wails >/dev/null 2>&1 || { echo "❌ 未找到 wails CLI，请先安装: go install github.com/wailsapp/wails/v2/cmd/wails@latest"; exit 1; }
command -v clang >/dev/null 2>&1 || { echo "❌ 未找到 clang"; exit 1; }
echo "  ✅ wails $(wails version 2>/dev/null | head -1)"
echo "  ✅ clang"

# 2. 构建前端
echo ""
echo "[2/5] 构建前端..."
cd frontend && npm install --silent && npm run build && cd ..
echo "  ✅ 前端构建完成"

# 3. Wails 构建（本机，无需交叉编译）
echo ""
echo "[3/5] Wails 构建 (darwin/arm64)..."
wails build \
  -platform darwin/arm64 \
  -tags "sqlite_load_extension fts5" \
  -clean \
  -trimpath
echo "  ✅ Go 构建完成"

# 4. 打包 vec0 扩展到 .app 内
echo ""
echo "[4/5] 打包 vec0 扩展..."
APP_BUNDLE="build/bin/${APP_NAME}.app"
EXE_DIR="${APP_BUNDLE}/Contents/MacOS"

if [ ! -f "ext/darwin/vec0.dylib" ]; then
  echo "  ⚠️  ext/darwin/vec0.dylib 不存在，向量/RAG 功能将不可用"
else
  mkdir -p "${EXE_DIR}/ext/darwin"
  cp ext/darwin/vec0.dylib "${EXE_DIR}/ext/darwin/vec0.dylib"
  echo "  ✅ vec0.dylib 已嵌入 ${EXE_DIR}/ext/darwin/"
fi

# 5. 生成 zip 安装包
echo ""
echo "[5/5] 生成安装包..."
rm -rf "$RELEASE_DIR"
mkdir -p "$RELEASE_DIR"

ZIP_NAME="AgentForge-${VERSION}-darwin-arm64.zip"
cd build/bin && ditto -c -k --keepParent "${APP_NAME}.app" "${RELEASE_DIR}/${ZIP_NAME}" && cd ../..
echo "  ✅ ${RELEASE_DIR}/${ZIP_NAME}"

echo ""
echo "=========================================="
echo "  ✅ 打包完成！"
echo "  产物: release/darwin-arm64/${ZIP_NAME}"
echo "=========================================="
