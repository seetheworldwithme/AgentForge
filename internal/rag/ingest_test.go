package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag/parser"
	"github.com/agent-rust/core/internal/store"
)

// TestGenerateQA 验证 QA 索引模式的问答对解析：按行、竖线分隔、过滤空。
func TestGenerateQA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"什么是RAG|RAG是检索增强生成\nRAG有什么用|提升事实准确性\n空行|\n|缺问题"}}]}`))
	}))
	defer srv.Close()
	chat := llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "m"})
	ing := &Ingestor{Chat: chat}
	qas := ing.generateQA(context.Background(), "d1", "文本")
	if len(qas) != 2 {
		t.Fatalf("got %d qas, want 2: %+v", len(qas), qas)
	}
	if qas[0].q != "什么是RAG" || qas[0].a != "RAG是检索增强生成" {
		t.Errorf("qa[0]=%+v", qas[0])
	}
}

// TestGenerateQANoChat 验证未配 chat 时返回 nil（回退 content 子块）。
func TestGenerateQANoChat(t *testing.T) {
	ing := &Ingestor{}
	if qas := ing.generateQA(context.Background(), "d1", "文本"); qas != nil {
		t.Errorf("no chat should return nil, got %+v", qas)
	}
}

// TestIngestFileFull 验证全量入库(resumeFrom=0)：进度 total>0、done==total、status=ready。
func TestIngestFileFull(t *testing.T) {
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按输入数量返回向量（ingest 批量嵌入）
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"embedding": []float32{1.0, 0.0, 0.0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer embedSrv.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "ing.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{ID: "pe", BaseURL: embedSrv.URL, EmbedModel: "m", Kind: "embed", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateKB(store.KnowledgeBase{ID: "kb_i", EmbedProviderID: "pe", CreatedAt: now})
	_ = db.CreateDocument(store.Document{ID: "d_i", KBID: "kb_i", Filename: "f.txt", Status: "processing", CreatedAt: now})

	text := strings.Repeat("这是一段用于测试入库的中文文本内容。", 200)
	ing := &Ingestor{DB: db, Embed: llm.NewOpenAIClient(llm.Config{BaseURL: embedSrv.URL, Model: "m"}), KBID: "kb_i", EmbedModel: "m"}
	if err := ing.IngestFile(context.Background(), "d_i", "f.txt", "text/plain", []byte(text), 0); err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	doc, _ := db.GetDocument("d_i")
	if doc.Status != "ready" {
		t.Errorf("status=%s, want ready", doc.Status)
	}
	if doc.ChunkTotal <= 0 {
		t.Errorf("ChunkTotal=%d, want >0", doc.ChunkTotal)
	}
	if doc.ChunkDone != doc.ChunkTotal {
		t.Errorf("done=%d != total=%d", doc.ChunkDone, doc.ChunkTotal)
	}
}

// pngBytes 把一张图编码成 PNG 字节，用于构造测试图片。
func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// TestPrepareImageForVision 验证发送前的图片预处理：大图等比缩小到长边≤上限、
// 字节在预算内、输出是可解码的合法图像；小图不被放大。
func TestPrepareImageForVision(t *testing.T) {
	// 3000x2000 的大图 → 应缩到 1568x1045。
	big := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 3000; x++ {
			big.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	out, err := prepareImageForVision(pngBytes(t, big))
	if err != nil {
		t.Fatalf("prepareImageForVision: %v", err)
	}
	if len(out) > visionImageMaxBytes {
		t.Errorf("output %d bytes > %d budget", len(out), visionImageMaxBytes)
	}
	dec, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v (not a valid image)", err)
	}
	if w, h := dec.Bounds().Dx(), dec.Bounds().Dy(); w != 1568 || h != 1045 {
		t.Errorf("big image dims = %dx%d, want 1568x1045", w, h)
	}

	// 100x80 的小图 → 不应被放大。
	small := image.NewRGBA(image.Rect(0, 0, 100, 80))
	out2, err := prepareImageForVision(pngBytes(t, small))
	if err != nil {
		t.Fatalf("prepareImageForVision small: %v", err)
	}
	dec2, _, err := image.Decode(bytes.NewReader(out2))
	if err != nil {
		t.Fatalf("decode small output: %v", err)
	}
	if w, h := dec2.Bounds().Dx(), dec2.Bounds().Dy(); w > 100 || h > 80 {
		t.Errorf("small image was upscaled to %dx%d", w, h)
	}
}

// TestDescribeOneImageTimeout 验证单图超时：mock VLM 慢响应时，注入短超时让请求
// 在阈值内失败（而非等到慢响应），避免一张图卡死整文档。
func TestDescribeOneImageTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "img.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ing := &Ingestor{
		DB:                 db,
		Vision:             llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "m"}),
		visionImageTimeout: 50 * time.Millisecond,
	}
	start := time.Now()
	_, err = ing.describeOneImage(context.Background(),
		parser.ParsedImage{Data: pngBytes(t, image.NewRGBA(image.Rect(0, 0, 10, 10))), MIME: "image/png"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("timeout too slow: %v (expected ~50ms)", elapsed)
	}
}

// TestDescribeOneImageOK 验证正常路径：mock VLM 返回描述，describeOneImage 透传内容。
func TestDescribeOneImageOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"图里的文字"}}]}`))
	}))
	defer srv.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "img.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ing := &Ingestor{DB: db, Vision: llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "m"})}
	desc, err := ing.describeOneImage(context.Background(),
		parser.ParsedImage{Data: pngBytes(t, image.NewRGBA(image.Rect(0, 0, 20, 20))), MIME: "image/png"})
	if err != nil {
		t.Fatalf("describeOneImage: %v", err)
	}
	if desc != "图里的文字" {
		t.Errorf("desc=%q, want %q", desc, "图里的文字")
	}
}

// TestPrepareImageForVisionCorrupt 验证损坏/不支持的字节返回 error 而非 panic。
func TestPrepareImageForVisionCorrupt(t *testing.T) {
	if _, err := prepareImageForVision([]byte("not an image")); err == nil {
		t.Fatal("expected error for corrupt bytes, got nil")
	}
}

// TestPrepareImageForVisionAlpha 验证透明区域合成到白底（而非 JPEG 常见的变黑）。
func TestPrepareImageForVisionAlpha(t *testing.T) {
	// 全透明 10x10（NewRGBA 初始全 0，即 alpha=0）。合成到白底后应输出近白像素。
	out, err := prepareImageForVision(pngBytes(t, image.NewRGBA(image.Rect(0, 0, 10, 10))))
	if err != nil {
		t.Fatalf("prepareImageForVision alpha: %v", err)
	}
	dec, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	r, g, b, _ := dec.At(5, 5).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent pixel composited to (%d,%d,%d), want near-white (JPEG lossy)", r>>8, g>>8, b>>8)
	}
}

// TestDescribeImagesAll 验证不再有数量上限：超过旧上限(20)的图片也会全部被描述。
func TestDescribeImagesAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"desc"}}]}`))
	}))
	defer srv.Close()

	const n = 25
	imgs := make([]parser.ParsedImage, n)
	for i := range imgs {
		imgs[i] = parser.ParsedImage{Data: pngBytes(t, image.NewRGBA(image.Rect(0, 0, 10, 10))), MIME: "image/png"}
	}
	ing := &Ingestor{Vision: llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "m"})}
	descs := ing.describeImages(context.Background(), "d1", imgs)

	got := 0
	for _, d := range descs {
		if d != "" {
			got++
		}
	}
	if got != n {
		t.Errorf("described %d/%d images, want all %d (no cap)", got, n, n)
	}
}

// TestDescribeOneImageCache 验证图片描述缓存：同一张图第二次描述命中缓存，不再调 VLM
// （慢模型/大文档跨次入库时复用，是修复"图片阶段 ctx 超时死循环"的关键）。
func TestDescribeOneImageCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"desc-x"}}]}`))
	}))
	defer srv.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ing := &Ingestor{
		DB:          db,
		Vision:      llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "vlm-x"}),
		VisionModel: "vlm-x",
	}
	img := parser.ParsedImage{Data: pngBytes(t, image.NewRGBA(image.Rect(0, 0, 20, 20))), MIME: "image/png"}

	d1, err := ing.describeOneImage(context.Background(), img)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	d2, err := ing.describeOneImage(context.Background(), img)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if d1 != "desc-x" || d2 != "desc-x" {
		t.Errorf("descs=%q/%q, want desc-x both", d1, d2)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("VLM calls=%d, want 1 (second should hit cache)", got)
	}
}
