package server

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"columba.local/backend/storage"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
)

const panelCookie = "columba_session"
const panelSessionLifetime = 24 * time.Hour

type panelSession struct {
	passwordHash string
	expires      time.Time
}

type panelAuth struct {
	mu       sync.Mutex
	sessions map[string]panelSession
	attempts []time.Time
}

func passwordParts(encoded string) ([]byte, []byte, bool) {
	salt, hash, ok := strings.Cut(encoded, ":")
	s, e1 := hex.DecodeString(salt)
	h, e2 := hex.DecodeString(hash)
	return s, h, ok && e1 == nil && e2 == nil && len(s) == 16 && len(h) == 32
}

func hashPanelPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash), err
}

func checkPanelPassword(encoded, password string) bool {
	salt, expected, ok := passwordParts(encoded)
	if !ok {
		return false
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return err == nil && subtle.ConstantTimeCompare(hash, expected) == 1
}

func (s *Server) panelPassword(ctx context.Context, c *app.RequestContext) (string, bool) {
	store, ok := s.requireStore(ctx, c)
	if !ok {
		return "", false
	}
	var hash string
	found, err := store.GetSetting(ctx, storage.PanelPasswordSetting, &hash)
	_, _, valid := passwordParts(hash)
	if err != nil || found && !valid {
		respondFail(ctx, c, 503, "auth_unavailable", "无法读取登录配置")
		return "", false
	}
	return hash, true
}

func (a *panelAuth) authenticated(c *app.RequestContext, passwordHash string) bool {
	key := string(c.Cookie(panelCookie))
	session, ok := a.sessions[key]
	if !ok || passwordHash == "" || session.passwordHash != passwordHash || !time.Now().Before(session.expires) {
		delete(a.sessions, key)
		return false
	}
	return true
}

func (s *Server) panelAuthMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("Cache-Control", "no-store")
		s.panel.mu.Lock()
		hash, ok := s.panelPassword(ctx, c)
		authenticated := ok && s.panel.authenticated(c, hash)
		s.panel.mu.Unlock()
		if !authenticated {
			if ok {
				respondFail(ctx, c, 401, "unauthorized", "请先登录")
			}
			c.Abort()
			return
		}
		c.Next(ctx)
	}
}

func (s *Server) panelSessionStatus(ctx context.Context, c *app.RequestContext) {
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	hash, ok := s.panelPassword(ctx, c)
	if ok {
		respondOK(ctx, c, map[string]bool{"authenticated": s.panel.authenticated(c, hash), "setupRequired": hash == "", "setupAllowed": isLoopbackRequest(c) && localAuthority(string(c.Host()))})
	}
}

// Limit password work globally; forwarded IP headers cannot bypass this bound.
func (a *panelAuth) allowAttempt() bool {
	now := time.Now()
	for len(a.attempts) > 0 && now.Sub(a.attempts[0]) >= time.Minute {
		a.attempts = a.attempts[1:]
	}
	if len(a.attempts) >= 5 {
		return false
	}
	a.attempts = append(a.attempts, now)
	return true
}

func setPanelCookie(c *app.RequestContext, value string, maxAge int) {
	secure := string(c.URI().Scheme()) == "https"
	c.SetCookie(panelCookie, value, maxAge, "/", "", protocol.CookieSameSiteStrictMode, secure, true)
}

func (s *Server) panelLogin(ctx context.Context, c *app.RequestContext) {
	s.panelPasswordAction(ctx, c, false)
}

func (s *Server) panelChangePassword(ctx context.Context, c *app.RequestContext) {
	s.panelPasswordAction(ctx, c, true)
}

func (s *Server) panelPasswordAction(ctx context.Context, c *app.RequestContext, change bool) {
	s.panel.mu.Lock()
	allowed := s.panel.allowAttempt()
	s.panel.mu.Unlock()
	if !allowed {
		c.Header("Retry-After", "60")
		respondFail(ctx, c, 429, "too_many_attempts", "尝试过于频繁，请稍后再试")
		return
	}
	var input struct {
		Password        string `json:"password"`
		CurrentPassword string `json:"currentPassword"`
	}
	// A slow password upload must not hold the authentication lock or wait indefinitely.
	_ = c.GetConn().SetReadDeadline(time.Now().Add(5 * time.Second))
	body, err := readBoundedBody(requestBody(c), 4096)
	_ = c.GetConn().SetReadDeadline(time.Time{})
	if err != nil || json.Unmarshal(body, &input) != nil || len(input.Password) > 1024 || len(input.CurrentPassword) > 1024 {
		respondFail(ctx, c, 400, "invalid_password", "密码格式无效")
		return
	}
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	hash, ok := s.panelPassword(ctx, c)
	if !ok {
		return
	}
	if change && !s.panel.authenticated(c, hash) {
		respondFail(ctx, c, 401, "unauthorized", "请先登录")
		return
	}
	if hash == "" && (!isLoopbackRequest(c) || !localAuthority(string(c.Host()))) {
		respondFail(ctx, c, 403, "local_setup_required", "请在本机设置管理密码")
		return
	}
	password := input.Password
	if change {
		password = input.CurrentPassword
	}
	if hash != "" && !checkPanelPassword(hash, password) {
		respondFail(ctx, c, 400, "invalid_credentials", "密码不正确")
		return
	}
	if hash == "" || change {
		if utf8.RuneCountInString(input.Password) < 12 || strings.TrimSpace(input.Password) == "" {
			respondFail(ctx, c, 400, "weak_password", "请使用至少 12 个字符的密码")
			return
		}
		fresh, err := hashPanelPassword(input.Password)
		if err != nil {
			respondFail(ctx, c, 500, "auth_unavailable", "无法保存密码")
			return
		}
		updated, err := s.store.SavePanelPassword(ctx, hash, fresh)
		if err != nil {
			respondFail(ctx, c, 503, "auth_unavailable", "无法保存密码")
			return
		}
		if !updated {
			respondFail(ctx, c, 409, "password_changed", "登录配置已变更，请刷新后重试")
			return
		}
		hash = fresh
		clear(s.panel.sessions)
	}
	// Every login rotates the presented session. Sessions are bounded and expire after a day.
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		respondFail(ctx, c, 500, "auth_unavailable", "无法创建登录会话")
		return
	}
	if s.panel.sessions == nil {
		s.panel.sessions = make(map[string]panelSession)
	}
	delete(s.panel.sessions, string(c.Cookie(panelCookie)))
	for key, session := range s.panel.sessions {
		if !time.Now().Before(session.expires) {
			delete(s.panel.sessions, key)
		}
	}
	if len(s.panel.sessions) >= 32 {
		var oldest string
		for key, session := range s.panel.sessions {
			if oldest == "" || session.expires.Before(s.panel.sessions[oldest].expires) {
				oldest = key
			}
		}
		delete(s.panel.sessions, oldest)
	}
	token := hex.EncodeToString(nonce)
	s.panel.sessions[token] = panelSession{hash, time.Now().Add(panelSessionLifetime)}
	setPanelCookie(c, token, int(panelSessionLifetime.Seconds()))
	respondOK(ctx, c, map[string]bool{"authenticated": true, "setupRequired": false})
}

func (s *Server) panelLogout(ctx context.Context, c *app.RequestContext) {
	s.panel.mu.Lock()
	delete(s.panel.sessions, string(c.Cookie(panelCookie)))
	s.panel.mu.Unlock()
	setPanelCookie(c, "", -1)
	respondOK(ctx, c, map[string]bool{"authenticated": false})
}
