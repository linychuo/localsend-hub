package state

import (
	"context"
	"testing"
	"time"
)

// TestSessionTTL_LongTransferKeptAlive 回归测试：长时间跨度 (>sessionTTL) 的多文件批次传输
// 不应因僵尸 session 清理器误删活跃 session 而导致后续文件 403。
//
// 修复前：sweep 使用创建时间判断过期，注册超过 sessionTTL 的 session 会被清掉。
// 修复后：sweep 使用最近活跃时间 (RegisterUploadCancel / CleanupUpload 都会刷新)，活跃 session 永不过期。
func TestSessionTTL_LongTransferKeptAlive(t *testing.T) {
	s := NewForTesting(t.TempDir())

	// 准备一个 session，包含 3 个文件 (模拟批次传输)
	sessionID := "session-long-transfer"
	files := map[string]*FileMeta{
		"file1": {FileName: "a.mov"},
		"file2": {FileName: "b.mov"},
		"file3": {FileName: "c.mov"},
	}
	tokens := map[string]string{
		"file1": "token1", "file2": "token2", "file3": "token3",
	}
	s.RegisterSession(sessionID, files, tokens)

	// 模拟时间流逝：session 已注册很久 (远超过 sessionTTL)
	// 把 sessionLastActive 倒回到 sessionTTL 之前
	longAgo := time.Now().Add(-2 * sessionTTL)
	s.mu.Lock()
	s.sessionLastActive[sessionID] = longAgo
	s.mu.Unlock()

	// 第一个文件开始上传 —— 应当刷新活跃时间
	cancel1 := func() {}
	s.RegisterUploadCancel(sessionID, "file1", cancel1)

	// 校验：上传开始后，sessionLastActive 被刷新到现在附近
	s.mu.Lock()
	lastActive := s.sessionLastActive[sessionID]
	s.mu.Unlock()
	if time.Since(lastActive) > time.Second {
		t.Fatalf("RegisterUploadCancel should have refreshed sessionLastActive to ~now, but it's %v ago", time.Since(lastActive))
	}

	// 执行一轮 sweep：活跃 session 不应被清理
	cleaned := s.sweepExpiredSessionsOnce(time.Now(), sessionTTL)
	if cleaned != 0 {
		t.Fatalf("sweep deleted an active session (count=%d); long-running transfers would lose tokens", cleaned)
	}

	// 校验 token 仍可用 (这是用户在长传输中后期文件能继续成功的关键)
	if !s.ValidateToken(sessionID, "file2", "token2") {
		t.Fatalf("session token for file2 was lost; mid-transfer files would get 403 Invalid token")
	}
	if !s.ValidateToken(sessionID, "file3", "token3") {
		t.Fatalf("session token for file3 was lost; mid-transfer files would get 403 Invalid token")
	}
}

// TestSessionTTL_IdleSessionCleaned 验证：真正空闲超过 TTL 的僵尸 session 仍会被清理
func TestSessionTTL_IdleSessionCleaned(t *testing.T) {
	s := NewForTesting(t.TempDir())

	sessionID := "session-idle"
	files := map[string]*FileMeta{"file1": {FileName: "abandoned.mov"}}
	tokens := map[string]string{"file1": "tok"}
	s.RegisterSession(sessionID, files, tokens)

	// 上传从未开始，sessionLastActive 已是创建时间 (即 longAgo 之前)
	// 直接倒回到 TTL 之前
	s.mu.Lock()
	s.sessionLastActive[sessionID] = time.Now().Add(-2 * sessionTTL)
	s.mu.Unlock()

	cleaned := s.sweepExpiredSessionsOnce(time.Now(), sessionTTL)
	if cleaned != 1 {
		t.Fatalf("expected 1 idle session to be cleaned, got %d", cleaned)
	}

	// 校验 session 已彻底清理 (token 应当失效)
	if s.ValidateToken(sessionID, "file1", "tok") {
		t.Fatalf("idle session was not fully cleaned; tokens still validate")
	}
}

// TestSessionTTL_UploadInProgressProtected 验证：即使 lastActive 超过 TTL，正在进行中的上传
// (uploadCancelFuncs 非空) 仍受保护，不会被 sweep 删除
func TestSessionTTL_UploadInProgressProtected(t *testing.T) {
	s := NewForTesting(t.TempDir())

	sessionID := "session-slow-upload"
	files := map[string]*FileMeta{"big.mov": {FileName: "big.mov"}}
	tokens := map[string]string{"big.mov": "tok"}
	s.RegisterSession(sessionID, files, tokens)
	s.RegisterUploadCancel(sessionID, "big.mov", func() {})

	// 倒回 lastActive 到 TTL 之前 (模拟上传耗时很久，期间 sweep 被卡在 uploadCancelFuncs 检查上)
	s.mu.Lock()
	s.sessionLastActive[sessionID] = time.Now().Add(-3 * sessionTTL)
	s.mu.Unlock()

	cleaned := s.sweepExpiredSessionsOnce(time.Now(), sessionTTL)
	if cleaned != 0 {
		t.Fatalf("in-flight upload session should be protected from sweep, but %d sessions were cleaned", cleaned)
	}

	// 完成后清理
	s.CleanupUpload(sessionID, "big.mov")
}

// TestSessionTTL_CleanupUploadTouchesActiveSession 验证：CleanupUpload 在 session 还有未完成文件时
// 也会刷新 sessionLastActive，防止批次传输中文件间空闲间隙被误删
func TestSessionTTL_CleanupUploadTouchesActiveSession(t *testing.T) {
	s := NewForTesting(t.TempDir())

	sessionID := "session-batch"
	files := map[string]*FileMeta{
		"a": {FileName: "a.mov"},
		"b": {FileName: "b.mov"},
		"c": {FileName: "c.mov"},
	}
	tokens := map[string]string{"a": "ta", "b": "tb", "c": "tc"}
	s.RegisterSession(sessionID, files, tokens)
	s.RegisterUploadCancel(sessionID, "a", func() {})

	// 第一个文件 a 开始时刚 touch 过时间 —— 现在模拟 a 完成
	// CleanupUpload(a) 后 session 还有 b、c 未完成，应当再次 touch
	s.mu.Lock()
	s.sessionLastActive[sessionID] = time.Now().Add(-2 * sessionTTL)
	s.mu.Unlock()

	s.CleanupUpload(sessionID, "a")

	s.mu.Lock()
	lastActive := s.sessionLastActive[sessionID]
	s.mu.Unlock()
	if time.Since(lastActive) > time.Second {
		t.Fatalf("CleanupUpload should have refreshed sessionLastActive while session still has pending files; "+
			"but it's %v ago (subsequent files in the batch would be swept)", time.Since(lastActive))
	}
}

// TestSessionTTL_LastFileCleanupDeletesSession 验证：session 最后一个文件 CleanupUpload 后，
// session 完全清理 (从 sessionLastActive 也删除)，避免内存泄漏
func TestSessionTTL_LastFileCleanupDeletesSession(t *testing.T) {
	s := NewForTesting(t.TempDir())

	sessionID := "session-single"
	files := map[string]*FileMeta{"only": {FileName: "only.mov"}}
	tokens := map[string]string{"only": "tok"}
	s.RegisterSession(sessionID, files, tokens)
	s.RegisterUploadCancel(sessionID, "only", func() {})

	s.CleanupUpload(sessionID, "only")

	s.mu.Lock()
	_, exists := s.sessionLastActive[sessionID]
	s.mu.Unlock()
	if exists {
		t.Fatalf("sessionLastActive entry should be removed after the last file is cleaned up")
	}
}

// TestSessionTTL_CancelSessionDeletesLastActive 验证：CancelSession 同时清理 sessionLastActive
func TestSessionTTL_CancelSessionDeletesLastActive(t *testing.T) {
	s := NewForTesting(t.TempDir())

	sessionID := "session-cancelled"
	files := map[string]*FileMeta{"f": {FileName: "f.mov"}}
	tokens := map[string]string{"f": "tok"}
	s.RegisterSession(sessionID, files, tokens)
	s.RegisterUploadCancel(sessionID, "f", func() {})

	s.CancelSession(sessionID)

	s.mu.Lock()
	_, exists := s.sessionLastActive[sessionID]
	s.mu.Unlock()
	if exists {
		t.Fatalf("sessionLastActive entry should be removed by CancelSession")
	}
}

// 防止 lint 标记 context 导入未使用 (RegisterUploadCancel 签名要求 context.CancelFunc)
var _ = context.Background