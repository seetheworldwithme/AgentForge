"""
Qwen3-VL-Embedding-2B 多模态 Embedding API 服务

- 启动时一次性加载模型并常驻内存（其他服务可持续调用）
- POST /embed   : 对「文本 / 图片(base64) / 图文混合」批量生成 embedding
- GET  /healthz : 健康检查
- GET  /        : 服务信息；GET /docs 提供 Swagger 交互页面

启动方式（在 conda env `sentence-transformers` 下，模型目录里执行）：
    uvicorn api_server:app --host 0.0.0.0 --port 8000
或：
    python api_server.py
"""

import os
import io
import sys
import time
import base64
import logging
import threading
from typing import List, Optional
from contextlib import asynccontextmanager

import torch
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field

# 让 Python 能找到 scripts/ 下的官方 embedder
_HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, _HERE)
sys.path.insert(0, os.path.join(_HERE, "scripts"))

from scripts.qwen3_vl_embedding import Qwen3VLEmbedder  # noqa: E402

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("qwen3vl-embed")

# 模型路径就是本文件所在目录
MODEL_PATH = _HERE
# Mac 上优先用 MPS 加速，否则回退 CPU
DEVICE = "mps" if torch.backends.mps.is_available() else "cpu"
PORT = int(os.environ.get("PORT", "8000"))

# 全局模型实例 + 推理锁（单设备上串行推理，避免并发显存/MPS 冲突）
embedder: Optional[Qwen3VLEmbedder] = None
infer_lock = threading.Lock()


@asynccontextmanager
async def lifespan(_app: FastAPI):
    """FastAPI 生命周期：启动时加载模型，关闭时清理。"""
    global embedder
    logger.info("加载模型中... path=%s device=%s", MODEL_PATH, DEVICE)
    t0 = time.time()
    embedder = Qwen3VLEmbedder(model_name_or_path=MODEL_PATH)
    if DEVICE == "mps":
        embedder.model = embedder.model.to("mps")
    embedder.model.eval()
    logger.info("模型加载完成，耗时 %.1fs", time.time() - t0)
    yield
    logger.info("服务关闭")


app = FastAPI(title="Qwen3-VL-Embedding-2B API", version="1.0", lifespan=lifespan)
# 允许跨域（方便其他服务 / 浏览器前端直接调用；内网服务可按需收紧）
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


# ---------------- 请求 / 响应结构 ----------------

class EmbedItem(BaseModel):
    text: Optional[str] = None
    image: Optional[str] = Field(
        None, description="图片 base64 字符串；可带 data:image/png;base64, 前缀"
    )
    instruction: Optional[str] = Field(
        None, description="该条输入的 instruction；不填则用请求级 instruction 或模型默认值"
    )


class EmbedRequest(BaseModel):
    inputs: List[EmbedItem]
    normalize: bool = True
    instruction: Optional[str] = Field(
        None,
        description="请求级默认 instruction；单个 item 未指定时使用。"
                    "检索场景建议 query 用 'Represent the query.'，文档用 'Represent the document.'",
    )


class EmbedResponse(BaseModel):
    embeddings: List[List[float]]
    dim: int
    count: int


# ---------------- 工具函数 ----------------

def decode_image(b64: str):
    """base64 字符串 -> PIL.Image (RGB)"""
    if b64.startswith("data:"):
        b64 = b64.split(",", 1)[1]
    try:
        raw = base64.b64decode(b64)
    except Exception as e:
        raise ValueError(f"base64 解码失败: {e}")
    from PIL import Image
    return Image.open(io.BytesIO(raw)).convert("RGB")


def build_inputs(req: EmbedRequest):
    """把请求体转成官方 embedder 需要的 List[dict] 结构。"""
    inputs = []
    for i, item in enumerate(req.inputs):
        if item.text is None and item.image is None:
            raise HTTPException(400, f"inputs[{i}] 的 text 和 image 至少给一个")
        entry = {}
        if item.text is not None:
            entry["text"] = item.text
        if item.image is not None:
            try:
                entry["image"] = decode_image(item.image)
            except ValueError as e:
                raise HTTPException(400, f"inputs[{i}] {e}")
        entry["instruction"] = item.instruction or req.instruction
        inputs.append(entry)
    return inputs


# ---------------- 路由 ----------------

@app.get("/")
def root():
    return {
        "service": "qwen3-vl-embedding-2b",
        "device": DEVICE,
        "model_loaded": embedder is not None,
        "docs": "/docs",
        "endpoints": ["/embed", "/healthz"],
    }


@app.get("/healthz")
def healthz():
    return {"status": "ok", "model_loaded": embedder is not None, "device": DEVICE}


@app.post("/embed", response_model=EmbedResponse)
def embed(req: EmbedRequest):
    if embedder is None:
        raise HTTPException(503, "model not loaded yet")
    if not req.inputs:
        raise HTTPException(400, "inputs is empty")

    inputs = build_inputs(req)

    # 串行推理，保证单设备上不并发抢占
    with infer_lock:
        emb = embedder.process(inputs, normalize=req.normalize)

    arr = emb.detach().cpu().float().numpy()
    return EmbedResponse(
        embeddings=arr.tolist(),
        dim=int(arr.shape[1]),
        count=int(arr.shape[0]),
    )


if __name__ == "__main__":
    import uvicorn
    uvicorn.run("api_server:app", host="0.0.0.0", port=PORT, workers=1)
