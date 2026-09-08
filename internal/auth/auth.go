package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "mikrollm_session"
	flashCookie   = "mikrollm_flash"
	cookiePath    = "/admin"
	sessionTTL    = 12 * time.Hour
	loginWindow   = 10 * time.Minute
	loginMaxFails = 5
)

type Service struct {
	st ports.AuthStore

	mu      sync.Mutex
	buckets map[int64]*bucket
	fails   map[string]*fail
	flashes map[string]flash
}

type bucket struct {
	window time.Time
	count  int
}

type fail struct {
	until time.Time
	n     int
}

type flash struct {
	val   string
	until time.Time
}

func New(st ports.AuthStore) *Service {
	return &Service{
		st: st, buckets: map[int64]*bucket{}, fails: map[string]*fail{}, flashes: map[string]flash{},
	}
}

func ClientIP(r *http.Request) string {
	if r == nil || r.RemoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func HashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func GenerateKey() (plain, prefix, hash string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return
	}
	plain = "sk-" + hex.EncodeToString(b)
	if len(plain) > 11 {
		prefix = plain[:11]
	} else {
		prefix = plain
	}
	hash = HashKey(plain)
	return
}

func GenerateMCPToken() (plain, prefix, hash string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return
	}
	plain = "mcp-" + hex.EncodeToString(b)
	if len(plain) > 11 {
		prefix = plain[:11]
	} else {
		prefix = plain
	}
	hash = HashKey(plain)
	return
}

func Bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if k := r.Header.Get("X-Api-Key"); k != "" {
		return k
	}
	return ""
}

func (s *Service) Authenticate(plain string) (domain.APIKey, error) {
	if plain == "" {
		return domain.APIKey{}, errAuth
	}
	k, err := s.st.GetKeyByHash(HashKey(plain))
	if err != nil {
		return domain.APIKey{}, errAuth
	}
	if !k.Enabled {
		return domain.APIKey{}, errAuth
	}
	return k, nil
}

func ModelAllowed(k domain.APIKey, model string) bool {
	return k.Allows(model)
}

func (s *Service) AllowRPM(k domain.APIKey) bool {
	if k.RPM <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.buckets[k.ID]
	now := time.Now()
	if b == nil || now.Sub(b.window) >= time.Minute {
		s.buckets[k.ID] = &bucket{window: now, count: 1}
		return true
	}
	if b.count >= k.RPM {
		return false
	}
	b.count++
	return true
}

var errAuth = &authError{"unauthorized"}

type authError struct{ s string }

func (e *authError) Error() string { return e.s }

func (s *Service) CheckPassword(pw string) bool {
	hash, err := s.st.AdminHash()
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func (s *Service) ValidMCP(token string) bool {
	token = domain.SanitizeToken(token)
	if token == "" {
		return false
	}
	hash, err := s.st.MCPTokenHash()
	if err == nil && len(hash) == 64 {
		got := HashKey(token)
		if subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1 {
			return true
		}
	}
	return s.CheckPassword(token)
}

func (s *Service) LoginBlocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneFailsLocked()
	f := s.fails[ip]
	if f == nil {
		return false
	}
	return f.n >= loginMaxFails
}

func (s *Service) RecordLogin(ip string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneFailsLocked()
	if ok {
		delete(s.fails, ip)
		return
	}
	f := s.fails[ip]
	if f == nil {
		if len(s.fails) > 8192 {
			s.fails = map[string]*fail{}
		}
		f = &fail{until: time.Now().Add(loginWindow)}
		s.fails[ip] = f
	}
	f.n++
}

func (s *Service) pruneFailsLocked() {
	now := time.Now()
	for ip, f := range s.fails {
		if now.After(f.until) {
			delete(s.fails, ip)
		}
	}
}

type session struct {
	Exp  int64  `json:"exp"`
	V    int    `json:"v"`
	CSRF string `json:"csrf"`
}

func (s *Service) IssueCookie(w http.ResponseWriter, r *http.Request) error {
	sec, err := s.st.SessionSecret()
	if err != nil {
		return err
	}
	csrf, err := randomHex(16)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(session{Exp: time.Now().Add(sessionTTL).Unix(), V: 2, CSRF: csrf})
	mac := hmac.New(sha256.New, []byte(sec))
	mac.Write(payload)
	val := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, sessionCookieValue(val, int(sessionTTL.Seconds()), r))
	return nil
}

func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: cookiePath, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: cookiePath, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *Service) ValidSession(r *http.Request) bool {
	_, ok := s.parseSession(r)
	return ok
}

func (s *Service) CSRF(r *http.Request) string {
	sess, ok := s.parseSession(r)
	if !ok {
		return ""
	}
	return sess.CSRF
}

func (s *Service) ValidCSRF(r *http.Request, tok string) bool {
	want := s.CSRF(r)
	if want == "" || tok == "" {
		return false
	}
	if len(want) != len(tok) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(tok)) == 1
}

func (s *Service) PutFlash(w http.ResponseWriter, val string) {
	id, err := randomHex(16)
	if err != nil {
		return
	}
	s.mu.Lock()
	if len(s.flashes) > 256 {
		s.flashes = map[string]flash{}
	}
	s.flashes[id] = flash{val: val, until: time.Now().Add(2 * time.Minute)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: id, Path: cookiePath, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 120,
	})
}

func (s *Service) TakeFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(flashCookie)
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: cookiePath, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	if err != nil || c.Value == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flashes[c.Value]
	delete(s.flashes, c.Value)
	if !ok || time.Now().After(f.until) {
		return ""
	}
	return f.val
}

func (s *Service) parseSession(r *http.Request) (session, bool) {
	var zero session
	if r == nil {
		return zero, false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return zero, false
	}
	sec, err := s.st.SessionSecret()
	if err != nil {
		return zero, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return zero, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, false
	}
	mac := hmac.New(sha256.New, []byte(sec))
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return zero, false
	}
	var sess session
	if json.Unmarshal(payload, &sess) != nil {
		return zero, false
	}
	if sess.V != 2 || sess.CSRF == "" || time.Now().Unix() >= sess.Exp {
		return zero, false
	}
	return sess, true
}

func sessionCookieValue(val string, maxAge int, r *http.Request) *http.Cookie {
	c := &http.Cookie{
		Name: sessionCookie, Value: val, Path: cookiePath,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
	if r != nil && r.TLS != nil {
		c.Secure = true
	}
	return c
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
