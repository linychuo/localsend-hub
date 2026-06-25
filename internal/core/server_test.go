package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localsend-hub/internal/state"
)

// newTestServer 构造一个可直接调用 handler 的 Server，不启动 TLS、不启动多播
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st := state.NewForTesting(dir)
	return &Server{
		state:       st,
		port:        53317,
		fingerprint: "AA" + strings.Repeat("0", 62), // 64-char hex 占位指纹
	}
}

// call 用 handler 直接发起请求，返回响应
func call(t *testing.T, s *Server, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	rr := httptest.NewRecorder()
	s.mux().ServeHTTP(rr, req)
	return rr
}

// prepareUpload 发起 prepare-upload 请求，返回解析后的响应
func prepareUpload(t *testing.T, s *Server, info map[string]interface{}, files map[string]interface{}) map[string]interface{} {
	t.Helper()
	payload := map[string]interface{}{"info": info, "files": files}
	data, _ := json.Marshal(payload)
	rr := call(t, s, http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(data))
	if rr.Code != 200 {
		t.Fatalf("prepare-upload expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("prepare-upload response parse error: %v", err)
	}
	return resp
}

// validFingerprint 是一个 64 字符 hex 串，能通过 validSenderFingerprint 校验
var validFingerprint = "0123456789ABCDEF" + strings.Repeat("0", 48)

func TestHandleInfo(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodGet, "/api/localsend/v2/info", nil)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if resp["fingerprint"] != s.fingerprint {
		t.Errorf("fingerprint = %v, want %s", resp["fingerprint"], s.fingerprint)
	}
	if resp["version"] != "2.0" {
		t.Errorf("version = %v, want 2.0", resp["version"])
	}
	if _, ok := resp["port"]; ok {
		t.Errorf("info response should not include port")
	}
}

func TestPrepareUpload_RejectsNonPOST(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodGet, "/api/localsend/v2/prepare-upload", nil)
	if rr.Code != 405 {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestPrepareUpload_RejectsEmptyFiles(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodPost, "/api/localsend/v2/prepare-upload",
		strings.NewReader(`{"info":{},"files":{}}`))
	if rr.Code != 400 {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPrepareUpload_ValidFingerprintStored(t *testing.T) {
	s := newTestServer(t)
	resp := prepareUpload(t, s,
		map[string]interface{}{"fingerprint": validFingerprint},
		map[string]interface{}{
			"f1": map[string]interface{}{"id": "f1", "fileName": "a.txt", "fileType": "text"},
		},
	)
	sid, _ := resp["sessionId"].(string)
	if sid == "" {
		t.Fatal("sessionId empty")
	}
	tokens, _ := resp["files"].(map[string]interface{})
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if _, ok := tokens["f1"]; !ok {
		t.Fatal("token for f1 missing")
	}

	// 元信息应包含校验过的 fingerprint
	meta := s.state.ResolveFileMeta(sid, "f1")
	if meta.SenderFingerprint != validFingerprint {
		t.Errorf("stored fingerprint = %q, want %q", meta.SenderFingerprint, validFingerprint)
	}
}

func TestPrepareUpload_InvalidFingerprintIgnored(t *testing.T) {
	cases := []string{
		"../../etc/cron.d",  // 路径穿越
		"short",             // 长度不对
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", // 64 字符但非 hex
		"",
	}
	for _, fp := range cases {
		t.Run(fp, func(t *testing.T) {
			s := newTestServer(t)
			resp := prepareUpload(t, s,
				map[string]interface{}{"fingerprint": fp},
				map[string]interface{}{
					"f1": map[string]interface{}{"id": "f1", "fileName": "a.txt", "fileType": "text"},
				},
			)
			sid, _ := resp["sessionId"].(string)
			meta := s.state.ResolveFileMeta(sid, "f1")
			if meta.SenderFingerprint != "" {
				t.Errorf("fingerprint %q should be rejected, got stored %q", fp, meta.SenderFingerprint)
			}
		})
	}
}

func TestUpload_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp := prepareUpload(t, s,
		map[string]interface{}{"fingerprint": validFingerprint},
		map[string]interface{}{
			"f1": map[string]interface{}{"id": "f1", "fileName": "hello.txt", "fileType": "text"},
		},
	)
	sid, _ := resp["sessionId"].(string)
	tokens, _ := resp["files"].(map[string]interface{})
	token, _ := tokens["f1"].(string)

	target := "/api/localsend/v2/upload?sessionId=" + sid + "&fileId=f1&token=" + token
	rr := call(t, s, http.MethodPost, target, strings.NewReader("hello world"))
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// 文件应落到 {receiveDir}/{fingerprint}/hello.txt
	rel := filepath.Join(s.state.GetReceiveDir(), validFingerprint, "hello.txt")
	got, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("file not at expected path %s: %v", rel, err)
	}
	if string(got) != "hello world" {
		t.Errorf("file content = %q, want %q", got, "hello world")
	}
}

func TestUpload_RejectsNonPOST(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodGet, "/api/localsend/v2/upload?sessionId=x&fileId=y&token=z", nil)
	if rr.Code != 405 {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestUpload_InvalidToken(t *testing.T) {
	s := newTestServer(t)
	resp := prepareUpload(t, s,
		map[string]interface{}{"fingerprint": validFingerprint},
		map[string]interface{}{
			"f1": map[string]interface{}{"id": "f1", "fileName": "a.txt", "fileType": "text"},
		},
	)
	sid, _ := resp["sessionId"].(string)

	// 用错的 token
	target := "/api/localsend/v2/upload?sessionId=" + sid + "&fileId=f1&token=wrong"
	rr := call(t, s, http.MethodPost, target, strings.NewReader("data"))
	if rr.Code != 403 {
		t.Fatalf("expected 403 for bad token, got %d", rr.Code)
	}
}

func TestUpload_MissingParams(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodPost, "/api/localsend/v2/upload", strings.NewReader("data"))
	if rr.Code != 400 {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUpload_PathTraversal_NeutralizedByFingerprintCheck(t *testing.T) {
	// 即使 prepare-upload 阶段绕过校验传入了恶意 fingerprint（防御性深度校验），
	// upload 阶段也应当拒绝用它构建路径。
	s := newTestServer(t)
	// 直接构造 state，绕过 prepare-upload 的校验
	st := s.state
	sid := "test-session"
	st.RegisterSession(sid, map[string]*state.FileMeta{
		"f1": {FileName: "evil.txt", SenderFingerprint: "../../etc/cron.d"},
	}, map[string]string{"f1": "tok"})

	target := "/api/localsend/v2/upload?sessionId=" + sid + "&fileId=f1&token=tok"
	rr := call(t, s, http.MethodPost, target, strings.NewReader("payload"))
	if rr.Code != 200 {
		t.Fatalf("expected 200 (file lands in root), got %d: %s", rr.Code, rr.Body.String())
	}

	// 关键断言：文件不应落到 /etc/cron.d，而应落到 receiveDir 根目录
	traversalPath := filepath.Join("/etc/cron.d", "evil.txt")
	if _, err := os.Stat(traversalPath); err == nil {
		t.Fatalf("file was written to %s — traversal not neutralized", traversalPath)
	}

	// 应当落到 receiveDir 根目录（因 fingerprint 校验失败，跳过子目录）
	safePath := filepath.Join(s.state.GetReceiveDir(), "evil.txt")
	if _, err := os.Stat(safePath); err != nil {
		t.Fatalf("file should land at %s: %v", safePath, err)
	}
}

func TestUpload_FilenameCollisionNotOverwritten(t *testing.T) {
	s := newTestServer(t)
	// 预先创建目标文件
	targetDir := filepath.Join(s.state.GetReceiveDir(), validFingerprint)
	os.MkdirAll(targetDir, 0755)
	existingPath := filepath.Join(targetDir, "dup.txt")
	os.WriteFile(existingPath, []byte("original"), 0644)

	// 发起同名上传
	resp := prepareUpload(t, s,
		map[string]interface{}{"fingerprint": validFingerprint},
		map[string]interface{}{
			"f1": map[string]interface{}{"id": "f1", "fileName": "dup.txt", "fileType": "text"},
		},
	)
	sid, _ := resp["sessionId"].(string)
	tokens, _ := resp["files"].(map[string]interface{})
	token, _ := tokens["f1"].(string)

	target := "/api/localsend/v2/upload?sessionId=" + sid + "&fileId=f1&token=" + token
	rr := call(t, s, http.MethodPost, target, strings.NewReader("new content"))
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// 原文件应未被覆盖
	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("original file unreadable: %v", err)
	}
	if string(got) != "original" {
		t.Errorf("original file overwritten, content = %q", got)
	}

	// 新文件应落到带时间戳的路径
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "dup_") && strings.HasSuffix(e.Name(), ".txt") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 timestamped file, got %d", count)
	}
}

func TestCancel_RejectsNonPOST(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodGet, "/api/localsend/v2/cancel?sessionId=x", nil)
	if rr.Code != 405 {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestCancel_MissingSessionId(t *testing.T) {
	s := newTestServer(t)
	rr := call(t, s, http.MethodPost, "/api/localsend/v2/cancel", nil)
	if rr.Code != 400 {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
