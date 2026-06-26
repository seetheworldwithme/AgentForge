#!/usr/bin/env bash
# 打包 Windows AMD64 安装包（从 macOS 交叉编译）
# 产物: release/windows-amd64/AgentForge-<version>-windows-amd64.zip
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

# 从 wails.json 读取版本号
VERSION=$(grep -o '"productVersion": *"[^"]*"' wails.json | head -1 | sed 's/.*": *"//;s/"//')
APP_NAME="agent-rust"
RELEASE_DIR="$ROOT_DIR/release/windows-amd64"

echo "=========================================="
echo "  AgentForge Windows AMD64 打包"
echo "  版本: $VERSION"
echo "=========================================="

# 1. 前置检查
echo ""
echo "[1/5] 检查依赖..."
command -v wails >/dev/null 2>&1 || { echo "❌ 未找到 wails CLI"; exit 1; }
command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 || { echo "❌ 未找到 x86_64-w64-mingw32-gcc，请安装: brew install mingw-w64"; exit 1; }
echo "  ✅ wails $(wails version 2>/dev/null | head -1)"
echo "  ✅ x86_64-w64-mingw32-gcc"

# 2. 构建前端
echo ""
echo "[2/5] 构建前端..."
cd frontend && npm install --silent && npm run build && cd ..
echo "  ✅ 前端构建完成"

# 3. Wails 交叉编译（CGO + mingw-w64）
echo ""
echo "[3/5] Wails 构建 (windows/amd64)..."
CGO_ENABLED=1 \
CC=x86_64-w64-mingw32-gcc \
CXX=x86_64-w64-mingw32-g++ \
wails build \
  -platform windows/amd64 \
  -tags "sqlite_load_extension" \
  -clean \
  -trimpath \
  -s
echo "  ✅ Go 构建完成"

# 4. 组织产物 + vec0 扩展
echo ""
echo "[4/5] 组织产物目录..."
BIN_DIR="$ROOT_DIR/build/bin"
PKG_DIR="$RELEASE_DIR/app"
rm -rf "$RELEASE_DIR"
mkdir -p "$PKG_DIR/ext/windows"

cp "${BIN_DIR}/${APP_NAME}.exe" "$PKG_DIR/"

if [ ! -f "ext/windows/vec0.dll" ]; then
  echo "  ⚠️  ext/windows/vec0.dll 不存在，向量/RAG 功能将不可用"
else
  cp ext/windows/vec0.dll "$PKG_DIR/ext/windows/vec0.dll"
  echo "  ✅ vec0.dll 已包含"
fi

# 5. 生成 zip 安装包
echo ""
echo "[5/5] 生成安装包..."
ZIP_NAME="AgentForge-${VERSION}-windows-amd64.zip"
cd "$RELEASE_DIR" && zip -r -q "$ZIP_NAME" app/ && cd "$ROOT_DIR"
echo "  ✅ ${RELEASE_DIR}/${ZIP_NAME}"

echo ""
echo "=========================================="
echo "  ✅ 打包完成！"
echo "  产物: release/windows-amd64/${ZIP_NAME}"
echo "  解压后直接运行 app/agent-rust.exe"
echo "=========================================="
