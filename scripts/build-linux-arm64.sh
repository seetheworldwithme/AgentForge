#!/usr/bin/env bash
# 打包 Linux ARM64 安装包（麒麟等信创系统）
# 从 macOS 交叉编译，产物: release/linux-arm64/AgentForge-<version>-linux-arm64.tar.gz
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

# 从 wails.json 读取版本号
VERSION=$(grep -o '"productVersion": *"[^"]*"' wails.json | head -1 | sed 's/.*": *"//;s/"//')
APP_NAME="agent-rust"
RELEASE_DIR="$ROOT_DIR/release/linux-arm64"

echo "=========================================="
echo "  AgentForge Linux ARM64 打包 (麒麟等信创)"
echo "  版本: $VERSION"
echo "=========================================="

# 1. 前置检查
echo ""
echo "[1/5] 检查依赖..."
command -v wails >/dev/null 2>&1 || { echo "❌ 未找到 wails CLI"; exit 1; }
command -v aarch64-linux-gnu-gcc >/dev/null 2>&1 || { echo "❌ 未找到 aarch64-linux-gnu-gcc，请安装: brew install aarch64-elf-gcc 或通过其他方式获取交叉编译器"; exit 1; }
echo "  ✅ wails $(wails version 2>/dev/null | head -1)"
echo "  ✅ aarch64-linux-gnu-gcc"

# 2. 构建前端
echo ""
echo "[2/5] 构建前端..."
cd frontend && npm install --silent && npm run build && cd ..
echo "  ✅ 前端构建完成"

# 3. Wails 交叉编译（CGO + aarch64-linux-gnu-gcc）
echo ""
echo "[3/5] Wails 构建 (linux/arm64)..."
CGO_ENABLED=1 \
CC=aarch64-linux-gnu-gcc \
CXX=aarch64-linux-gnu-g++ \
wails build \
  -platform linux/arm64 \
  -tags "sqlite_load_extension fts5" \
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
mkdir -p "$PKG_DIR/ext/linux"

cp "${BIN_DIR}/${APP_NAME}" "$PKG_DIR/"
chmod +x "$PKG_DIR/${APP_NAME}"

if [ ! -f "ext/linux/vec0.so" ]; then
  echo "  ⚠️  ext/linux/vec0.so 不存在！"
  echo "      向量/RAG 功能将不可用。请从 sqlite-vec 项目编译 Linux ARM64 版本："
  echo "      https://github.com/asg017/sqlite-vec"
  echo "      编译后将 vec0.so 放到 ext/linux/vec0.so，再重新运行本脚本"
else
  cp ext/linux/vec0.so "$PKG_DIR/ext/linux/vec0.so"
  echo "  ✅ vec0.so 已包含"
fi

# 生成 README 说明运行时依赖
cat > "$PKG_DIR/README.txt" << 'EOF'
AgentForge Linux ARM64
======================

运行时依赖（需在目标系统上安装）：

  # Ubuntu / Debian / 麒麟
  sudo apt install libgtk-3-0 libwebkit2gtk-4.0-37

  # 或如果包名不同（较新版本用 4.1）：
  sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0

运行方式：

  ./agent-rust

vec0.so 位置：放在 agent-rust 同级的 ext/linux/ 目录下。
若 vec0.so 缺失，向量检索 / RAG 功能不可用，其余功能正常。
EOF

# 5. 生成 tar.gz 安装包
echo ""
echo "[5/5] 生成安装包..."
TAR_NAME="AgentForge-${VERSION}-linux-arm64.tar.gz"
cd "$RELEASE_DIR" && tar czf "$TAR_NAME" app/ && cd "$ROOT_DIR"
echo "  ✅ ${RELEASE_DIR}/${TAR_NAME}"

echo ""
echo "=========================================="
echo "  ✅ 打包完成！"
echo "  产物: release/linux-arm64/${TAR_NAME}"
echo ""
echo "  ⚠️  目标系统需安装 WebKitGTK 运行时（详见包内 README.txt）"
[ ! -f "ext/linux/vec0.so" ] && echo "  ⚠️  vec0.so 缺失，请在目标系统编译后放入 ext/linux/"
echo "=========================================="
