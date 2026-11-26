// Package api exposes the portal HTTP API.
package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miaochi998/bnyc-remote/portal/internal/auth"
	"github.com/miaochi998/bnyc-remote/portal/internal/config"
	"github.com/miaochi998/bnyc-remote/portal/internal/store"
)

type ctxKey string

const userKey ctxKey = "portal_user"

// Server holds dependencies for HTTP handlers.
type Server struct {
	cfg *config.Config
	st  *store.Store
	lim *loginLimiter
}

// New constructs the API server.
func New(cfg *config.Config, st *store.Store) *Server {
	return &Server{cfg: cfg, st: st, lim: newLoginLimiter(5, time.Minute)}
}

// Routes registers all endpoints.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /admin", s.adminPage)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)

	mux.Handle("GET /api/devices", s.auth(s.listDevices))
	mux.Handle("POST /api/devices", s.auth(s.createDevice))
	mux.Handle("PUT /api/devices/{id}", s.auth(s.updateDevice))
	mux.Handle("DELETE /api/devices/{id}", s.auth(s.deleteDevice))

	mux.Handle("GET /api/admin/users", s.admin(s.listUsers))
	mux.Handle("POST /api/admin/users", s.admin(s.createUser))
	mux.Handle("POST /api/admin/users/{id}/reset-password", s.admin(s.resetPassword))

	return s.cors(mux)
}

// ---------- middleware ----------

func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := auth.ParseToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		u, err := s.st.GetUserByID(claims.UserID)
		if err != nil || u.Disabled {
			writeErr(w, http.StatusUnauthorized, "user unavailable")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func (s *Server) admin(next http.HandlerFunc) http.Handler {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil || u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "admin required")
			return
		}
		next(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	if s.cfg.AllowOrigins == "" {
		return next
	}
	allowed := map[string]bool{}
	for _, o := range strings.Split(s.cfg.AllowOrigins, ",") {
		allowed[strings.TrimSpace(o)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- handlers ----------

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.lim.allow(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try later")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "username and password required")
		return
	}
	u, hash, err := s.st.GetUserByUsername(body.Username)
	if err != nil || !auth.VerifyPassword(body.Password, hash) {
		s.lim.fail(ip)
		s.st.Audit(0, "login_failed", body.Username, ip)
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if u.Disabled {
		s.lim.fail(ip)
		writeErr(w, http.StatusForbidden, "account disabled")
		return
	}
	s.lim.reset(ip)
	token, err := auth.IssueToken(s.cfg.JWTSecret, u.ID, u.Username, u.Role, time.Duration(s.cfg.TokenTTLHrs)*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	s.st.Audit(u.ID, "login", "", ip)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	// Stateless JWT: client discards the token.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	devices, err := s.st.ListDevices(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	out := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		plain, decErr := decryptDevice(d.Secret, s.cfg.DeviceKey)
		if decErr != nil {
			plain = ""
		}
		out = append(out, map[string]any{
			"id":         d.ID,
			"name":       d.Name,
			"remote_id":  d.RemoteID,
			"note":       d.Note,
			"password":   plain,
			"created_at": d.CreatedAt,
			"updated_at": d.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body struct {
		Name     string `json:"name"`
		RemoteID string `json:"remote_id"`
		Password string `json:"password"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.RemoteID = strings.TrimSpace(body.RemoteID)
	if body.Name == "" || !validRemoteID(body.RemoteID) || len(body.Password) != 6 {
		writeErr(w, http.StatusBadRequest, "name, 9-digit remote_id and 6-char password required")
		return
	}
	nonce, cipherText, err := auth.EncryptSecret(body.Password, s.cfg.DeviceKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encrypt failed")
		return
	}
	id, err := s.st.CreateDevice(u.ID, body.Name, body.RemoteID, nonce, cipherText, body.Note)
	if err != nil {
		writeErr(w, http.StatusConflict, "device already exists or invalid")
		return
	}
	s.st.Audit(u.ID, "device_create", body.Name, clientIP(r))
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name     string `json:"name"`
		RemoteID string `json:"remote_id"`
		Password string `json:"password"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.RemoteID = strings.TrimSpace(body.RemoteID)
	if body.Name == "" || !validRemoteID(body.RemoteID) {
		writeErr(w, http.StatusBadRequest, "name and 9-digit remote_id required")
		return
	}
	var nonce, cipherText string
	if body.Password != "" {
		if len(body.Password) != 6 {
			writeErr(w, http.StatusBadRequest, "password must be 6 chars")
			return
		}
		nonce, cipherText, err = auth.EncryptSecret(body.Password, s.cfg.DeviceKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "encrypt failed")
			return
		}
	}
	if err := s.st.UpdateDevice(u.ID, id, body.Name, body.RemoteID, body.Note, nonce, cipherText); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "remote_id already in your address book")
			return
		}
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	s.st.Audit(u.ID, "device_update", body.Name, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.DeleteDevice(u.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.st.Audit(u.ID, "device_delete", strconv.FormatInt(id, 10), clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.st.ListUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	if body.Username == "" || len(body.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "username and password(>=8) required")
		return
	}
	role := "user"
	if body.Role == "admin" {
		role = "admin"
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	id, err := s.st.CreateUser(body.Username, hash, body.DisplayName, role)
	if err != nil {
		writeErr(w, http.StatusConflict, "username exists")
		return
	}
	admin := currentUser(r)
	s.st.Audit(admin.ID, "user_create", body.Username, clientIP(r))
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(body.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password(>=8) required")
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := s.st.SetPassword(id, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	admin := currentUser(r)
	s.st.Audit(admin.ID, "user_reset_password", strconv.FormatInt(id, 10), clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- helpers ----------

func currentUser(r *http.Request) *store.User {
	if u, ok := r.Context().Value(userKey).(*store.User); ok {
		return u
	}
	return nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func validRemoteID(s string) bool {
	if len(s) != 9 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func decryptDevice(secret string, key []byte) (string, error) {
	parts := strings.SplitN(secret, ":", 2)
	if len(parts) != 2 {
		return "", nil
	}
	return auth.DecryptSecret(parts[0], parts[1], key)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ---------- login limiter ----------

type loginLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string]*attempt
}

type attempt struct {
	count int
	first time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, attempts: map[string]*attempt{}}
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok {
		return true
	}
	if time.Since(a.first) > l.window {
		delete(l.attempts, ip)
		return true
	}
	return a.count < l.max
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Lazy cleanup: bound memory if many distinct IPs hit the login endpoint.
	if len(l.attempts) > 1024 {
		now := time.Now()
		for k, v := range l.attempts {
			if now.Sub(v.first) > l.window {
				delete(l.attempts, k)
			}
		}
	}
	a, ok := l.attempts[ip]
	if !ok || time.Since(a.first) > l.window {
		l.attempts[ip] = &attempt{count: 1, first: time.Now()}
		return
	}
	a.count++
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}
