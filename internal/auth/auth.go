package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	st ports.AuthStore

	mu      sync.Mutex
	buckets map[int64]*bucket
	fails   map[string]*fail
}

type bucket struct {
	window time.Time
	count  int
}

type fail struct {
	until time.Time
	n     int
}

func New(st ports.AuthStore) *Service {
	return &Service{st: st, buckets: map[int64]*bucket{}, fails: map[string]*fail{}}
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

func (s *Service) LoginBlocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fails[ip]
	if f == nil {
		return false
	}
	if time.Now().After(f.until) {
		delete(s.fails, ip)
		return false
	}
	return f.n >= 5
}

func (s *Service) RecordLogin(ip string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		delete(s.fails, ip)
		return
	}
	f := s.fails[ip]
	if f == nil || time.Now().After(f.until) {
		f = &fail{until: time.Now().Add(10 * time.Minute)}
		s.fails[ip] = f
	}
	f.n++
}

type session struct {
	Exp int64 `json:"exp"`
	V   int   `json:"v"`
}

func (s *Service) IssueCookie(w http.ResponseWriter, r *http.Request) error {
	sec, err := s.st.SessionSecret()
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(session{Exp: time.Now().Add(12 * time.Hour).Unix(), V: 1})
	mac := hmac.New(sha256.New, []byte(sec))
	mac.Write(payload)
	val := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     "mikrollm_session",
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 3600,
	})
	return nil
}

func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "mikrollm_session", Value: "", Path: "/", MaxAge: -1})
}

func (s *Service) ValidSession(r *http.Request) bool {
	c, err := r.Cookie("mikrollm_session")
	if err != nil {
		return false
	}
	sec, err := s.st.SessionSecret()
	if err != nil {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(sec))
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return false
	}
	var sess session
	if json.Unmarshal(payload, &sess) != nil {
		return false
	}
	return time.Now().Unix() < sess.Exp
}
