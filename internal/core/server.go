package core

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"localsend-hub/internal/discovery"
	"localsend-hub/internal/state"
)

// Server 核心 HTTPS 服务
type Server struct {
	state       *state.State
	port        int
	fingerprint string
	tlsCert     tls.Certificate
}

// NewServer 创建核心服务实例
func NewServer(st *state.State, port int) *Server {
	return &Server{state: st, port: port}
}

// Start 启动 HTTPS 服务器
func (s *Server) Start() error {
	if err := s.generateCert(); err != nil {
		return fmt.Errorf("cert generation failed: %w", err)
	}

	// 启动多播广播
	go discovery.NewAnnouncer(s.port, s.getDeviceInfo).Run()

	mux := http.NewServeMux()

	// 注册 LocalSend API 路由
	mux.HandleFunc("/api/localsend/v1/info", s.handleInfo)
	mux.HandleFunc("/api/localsend/v2/info", s.handleInfo)
	mux.HandleFunc("/api/localsend/v1/register", s.handleRegister)
	mux.HandleFunc("/api/localsend/v2/register", s.handleRegister)
	mux.HandleFunc("/api/localsend/v2/prepare-upload", s.handlePrepareUpload)
	mux.HandleFunc("/api/localsend/v2/upload", s.handleUpload)
	mux.HandleFunc("/api/localsend/v2/cancel", s.handleCancel)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🚀 Core Service listening on https://0.0.0.0%s", addr)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{s.tlsCert},
		},
	}

	return server.ListenAndServeTLS("", "")
}

// 生成自签名证书
func (s *Server) generateCert() error {
	log.Println("🔑 Generating RSA-2048 self-signed certificate...")
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "LocalSend Hub",
			Organization: []string{"LocalSend Hub"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24 * 365 * 10),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	// 计算指纹
	hash := sha256.Sum256(certDER)
	s.fingerprint = strings.ToUpper(hex.EncodeToString(hash[:]))

	// 构建 TLS 证书
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	s.tlsCert, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}

	log.Printf("🔐 Fingerprint: %s", s.fingerprint)
	return nil
}

// 获取设备信息函数 (供多播和 /info 使用)
func (s *Server) getDeviceInfo() map[string]interface{} {
	alias, model, deviceType := s.state.GetDeviceIdentity()
	return map[string]interface{}{
		"alias":       alias,
		"version":     "2.0",
		"deviceModel": model,
		"deviceType":  deviceType,
		"fingerprint": s.fingerprint,
		"port":        s.port,
		"protocol":    "https",
		"download":    false,
	}
}

// /info 响应 (不含 port/protocol)
func (s *Server) getInfoResponse() map[string]interface{} {
	alias, model, deviceType := s.state.GetDeviceIdentity()
	return map[string]interface{}{
		"alias":       alias,
		"version":     "2.0",
		"deviceModel": model,
		"deviceType":  deviceType,
		"fingerprint": s.fingerprint,
		"download":    false,
	}
}

// --- HTTP Handlers ---

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.getInfoResponse())
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// register 响应与 info 相同 (协议 spec)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.getInfoResponse())
}

func (s *Server) handlePrepareUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Info  map[string]interface{} `json:"info"`
		Files map[string]struct {
			ID       string `json:"id"`
			FileName string `json:"fileName"`
			Size     *int64 `json:"size"`
			FileType string `json:"fileType"`
			Sha256   *string `json:"sha256"`
			Preview  *string `json:"preview"`
			Metadata *struct {
				Modified  *string `json:"modified"`
				Accessed  *string `json:"accessed"`
			} `json:"metadata"`
		} `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	if req.Files == nil || len(req.Files) == 0 {
		http.Error(w, "No files specified", 400)
		return
	}

	// 提取发送设备的指纹
	senderFingerprint := ""
	if req.Info != nil {
		if fp, ok := req.Info["fingerprint"].(string); ok {
			senderFingerprint = fp
		}
	}

	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
	tokens := map[string]string{}
	fileMap := map[string]*state.FileMeta{}

	for id, info := range req.Files {
		tokens[id] = fmt.Sprintf("%d", time.Now().UnixNano())

		fileName := info.FileName
		if fileName == "" {
			fileName = info.ID
		}

		var modifiedTime *time.Time
		if info.Metadata != nil && info.Metadata.Modified != nil {
			if t, err := time.Parse(time.RFC3339, *info.Metadata.Modified); err == nil {
				modifiedTime = &t
			}
		}

		fileMap[id] = &state.FileMeta{
			FileName:        fileName,
			Size:            info.Size,
			FileType:        info.FileType,
			Sha256:          info.Sha256,
			Modified:        modifiedTime,
			SenderFingerprint: senderFingerprint,
		}
	}

	s.state.RegisterSession(sessionID, fileMap, tokens)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId": sessionID,
		"files":     tokens,
	})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sessionID := q.Get("sessionId")
	fileID := q.Get("fileId")
	token := q.Get("token")

	if sessionID == "" || fileID == "" || token == "" {
		http.Error(w, "Missing required query parameters (sessionId, fileId, token)", 400)
		return
	}

	// 验证 token
	if !s.state.ValidateToken(sessionID, fileID, token) {
		http.Error(w, "Invalid token", 403)
		return
	}

	meta := s.state.ResolveFileMeta(sessionID, fileID)
	fileName := filepath.Base(meta.FileName)
	if fileName == "" {
		fileName = filepath.Base(fileID)
	}

	// 创建可取消的 context，用于中途取消传输
	ctx, cancel := context.WithCancel(r.Context())
	s.state.RegisterUploadCancel(sessionID, cancel)
	defer func() {
		cancel()
		s.state.CleanupUpload(sessionID)
	}()

	// 根据文件元信息中的修改时间构建 YYYY/MM 目录结构
	dir := s.state.GetReceiveDir()
	
	// 按发送设备指纹创建子目录
	if meta.SenderFingerprint != "" {
		dir = filepath.Join(dir, meta.SenderFingerprint)
	}
	
	// 按文件修改时间创建年月子目录
	if meta.Modified != nil {
		yearMonth := meta.Modified.Format("2006/01")
		dir = filepath.Join(dir, yearMonth)
	}
	os.MkdirAll(dir, 0755)

	outPath := filepath.Join(dir, fileName)
	if _, err := os.Stat(outPath); err == nil {
		ext := filepath.Ext(fileName)
		base := strings.TrimSuffix(fileName, ext)
		outPath = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
	}

	f, err := os.Create(outPath)
	if err != nil {
		s.state.AddLog(fileName, 0, r.RemoteAddr, "Failed")
		http.Error(w, "Server Error", 500)
		return
	}
	defer f.Close()

	// 使用可取消的 reader 包装请求体
	cancellableReader := state.NewCancellableReader(ctx, r.Body)
	n, err := io.Copy(f, cancellableReader)
	if err != nil {
		if ctx.Err() != nil {
			// 传输被主动取消
			f.Close()
			os.Remove(outPath)
			s.state.AddLog(fileName, n, r.RemoteAddr, "Cancelled")
			log.Printf("❌ Upload cancelled: %s", fileName)
			http.Error(w, "Transfer cancelled", 499)
			return
		}
		s.state.AddLog(fileName, n, r.RemoteAddr, "Failed")
		http.Error(w, "Write Error", 500)
		return
	}

	log.Printf("📥 Received: %s (%d bytes)", outPath, n)
	s.state.AddLog(fileName, n, r.RemoteAddr, "Success")
	w.WriteHeader(200)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId", 400)
		return
	}

	s.state.CancelSession(sessionID)
	log.Printf("❌ Transfer cancelled: session %s", sessionID)

	w.WriteHeader(200)
}
