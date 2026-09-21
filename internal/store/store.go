package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

type Backend = domain.Backend
type Model = domain.Model
type APIKey = domain.APIKey
type RequestLog = domain.RequestLog

func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS backends (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  base_url TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  weight INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS models (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  alias TEXT NOT NULL UNIQUE,
  upstream_name TEXT NOT NULL,
  lb_policy TEXT NOT NULL DEFAULT 'least_conn',
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS model_backends (
  model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
  backend_id INTEGER NOT NULL REFERENCES backends(id) ON DELETE CASCADE,
  PRIMARY KEY (model_id, backend_id)
);
CREATE TABLE IF NOT EXISTS api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  allowed_models TEXT NOT NULL DEFAULT '["*"]',
  rpm INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  request_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS admin_meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  password_hash TEXT NOT NULL,
  session_secret TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS request_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  model TEXT NOT NULL,
  backend TEXT NOT NULL,
  status INTEGER NOT NULL,
  latency_ms INTEGER NOT NULL,
  bytes_out INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS request_log_ts ON request_log(ts);
CREATE TABLE IF NOT EXISTS ollama_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  backend_id INTEGER NOT NULL,
  backend TEXT NOT NULL,
  model TEXT NOT NULL,
  status TEXT NOT NULL,
  percent INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  log TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	_, _ = s.DB.Exec(`ALTER TABLE backends ADD COLUMN kind TEXT NOT NULL DEFAULT 'ollama'`)
	_, _ = s.DB.Exec(`ALTER TABLE backends ADD COLUMN token TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN max_context INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN fallback TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN prompt_cache TEXT NOT NULL DEFAULT 'inherit'`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN media TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN hub_share INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN hub_node_id TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE models ADD COLUMN hub_node_name TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN mcp_token_hash TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN mcp_token_prefix TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN prompt_cache TEXT NOT NULL DEFAULT 'auto'`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN hub_enabled INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN hub_node_id TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN hub_token TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN hub_name TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE admin_meta ADD COLUMN hub_schedule TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN upstream TEXT NOT NULL DEFAULT ''`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN prompt_usd REAL NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN cache_discount REAL NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN usage_cost REAL NOT NULL DEFAULT 0`)
	_, _ = s.DB.Exec(`ALTER TABLE request_log ADD COLUMN saved_usd REAL NOT NULL DEFAULT 0`)
	if _, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS billing_hour (
  hour TEXT PRIMARY KEY,
  n INTEGER NOT NULL DEFAULT 0,
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  cached_tokens INTEGER NOT NULL DEFAULT 0,
  usage_cost REAL NOT NULL DEFAULT 0,
  saved_usd REAL NOT NULL DEFAULT 0
);`); err != nil {
		return err
	}
	s.backfillBilling()
	if _, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS security_policies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL,
  action TEXT NOT NULL DEFAULT 'block',
  mode TEXT NOT NULL DEFAULT 'pre',
  enabled INTEGER NOT NULL DEFAULT 1,
  config TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS security_targets (
  policy_id INTEGER NOT NULL REFERENCES security_policies(id) ON DELETE CASCADE,
  target_kind TEXT NOT NULL,
  target_key TEXT NOT NULL,
  PRIMARY KEY (policy_id, target_kind, target_key)
);
`); err != nil {
		return err
	}
	_, err = s.DB.Exec(`
CREATE TABLE IF NOT EXISTS queues (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  alias TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  overflow_after INTEGER NOT NULL DEFAULT 5,
  overflow_alias TEXT NOT NULL DEFAULT '',
  max_wait_ms INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS queue_steps (
  queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
  pos INTEGER NOT NULL,
  model_alias TEXT NOT NULL,
  max_concurrent INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (queue_id, pos)
);
CREATE TABLE IF NOT EXISTS queue_aliases (
  alias TEXT PRIMARY KEY,
  queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS queue_jobs (
  id TEXT PRIMARY KEY,
  queue_id INTEGER NOT NULL,
  seq INTEGER NOT NULL,
  status TEXT NOT NULL,
  alias TEXT NOT NULL DEFAULT '',
  assigned_model TEXT NOT NULL DEFAULT '',
  assigned_backend TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  key_prefix TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  body BLOB,
  bytes INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS queue_jobs_q_status_seq ON queue_jobs(queue_id, status, seq);
CREATE INDEX IF NOT EXISTS queue_jobs_status ON queue_jobs(status);
`)
	return err
}

func (s *Store) EnsureAdmin(password string, reset bool) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_meta`).Scan(&n); err != nil {
		return err
	}
	if n > 0 && !reset {
		return nil
	}
	if password == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		password = hex.EncodeToString(b)
		log.Printf("generated admin password: %s", password)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	sec := make([]byte, 32)
	if _, err := rand.Read(sec); err != nil {
		return err
	}
	_, err = s.DB.Exec(`
INSERT INTO admin_meta (id, password_hash, session_secret) VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET password_hash=excluded.password_hash, session_secret=excluded.session_secret
`, string(hash), hex.EncodeToString(sec))
	return err
}

func (s *Store) AdminHash() (string, error) {
	var h string
	err := s.DB.QueryRow(`SELECT password_hash FROM admin_meta WHERE id=1`).Scan(&h)
	return h, err
}

func (s *Store) SessionSecret() (string, error) {
	var h string
	err := s.DB.QueryRow(`SELECT session_secret FROM admin_meta WHERE id=1`).Scan(&h)
	return h, err
}

func (s *Store) SetAdminPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	sec := make([]byte, 32)
	if _, err := rand.Read(sec); err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE admin_meta SET password_hash=?, session_secret=? WHERE id=1`, string(hash), hex.EncodeToString(sec))
	return err
}

func hashSecret(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func mcpPrefix(plain string) string {
	if len(plain) > 11 {
		return plain[:11]
	}
	return plain
}

func (s *Store) MCPTokenHash() (string, error) {
	var h string
	err := s.DB.QueryRow(`SELECT mcp_token_hash FROM admin_meta WHERE id=1`).Scan(&h)
	return h, err
}

func (s *Store) MCPTokenPrefix() (string, error) {
	var p string
	err := s.DB.QueryRow(`SELECT mcp_token_prefix FROM admin_meta WHERE id=1`).Scan(&p)
	return p, err
}

func (s *Store) SetMCPToken(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", fmt.Errorf("empty mcp token")
	}
	prefix := mcpPrefix(plain)
	_, err := s.DB.Exec(`UPDATE admin_meta SET mcp_token_hash=?, mcp_token_prefix=? WHERE id=1`, hashSecret(plain), prefix)
	return prefix, err
}

func (s *Store) EnsureMCPToken(plain string, reset bool) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain != "" {
		_, err := s.SetMCPToken(plain)
		return "", err
	}
	hash, err := s.MCPTokenHash()
	if err != nil {
		return "", err
	}
	if hash != "" && !reset {
		return "", nil
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	gen := "mcp-" + hex.EncodeToString(b)
	if _, err := s.SetMCPToken(gen); err != nil {
		return "", err
	}
	log.Printf("generated MCP token: %s", gen)
	return gen, nil
}

func (s *Store) ListBackends() ([]Backend, error) {
	rows, err := s.DB.Query(`SELECT id, name, base_url, enabled, weight, kind, token FROM backends ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Backend
	for rows.Next() {
		var b Backend
		var en int
		if err := rows.Scan(&b.ID, &b.Name, &b.BaseURL, &en, &b.Weight, &b.Kind, &b.Token); err != nil {
			return nil, err
		}
		b.Enabled = en == 1
		b.Kind = domain.NormalizeKind(b.Kind)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBackend(id int64) (Backend, error) {
	var b Backend
	var en int
	err := s.DB.QueryRow(`SELECT id, name, base_url, enabled, weight, kind, token FROM backends WHERE id=?`, id).
		Scan(&b.ID, &b.Name, &b.BaseURL, &en, &b.Weight, &b.Kind, &b.Token)
	b.Enabled = en == 1
	b.Kind = domain.NormalizeKind(b.Kind)
	return b, err
}

func (s *Store) UpsertBackend(name, baseURL string, enabled bool, weight int, kind, token string) (int64, error) {
	if weight <= 0 {
		weight = 1
	}
	en := 0
	if enabled {
		en = 1
	}
	kind = domain.NormalizeKind(kind)
	token = domain.SanitizeToken(token)
	res, err := s.DB.Exec(`
INSERT INTO backends (name, base_url, enabled, weight, kind, token) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(base_url) DO UPDATE SET name=excluded.name, enabled=excluded.enabled, weight=excluded.weight, kind=excluded.kind, token=excluded.token
`, name, strings.TrimRight(baseURL, "/"), en, weight, kind, token)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		_ = s.DB.QueryRow(`SELECT id FROM backends WHERE base_url=?`, strings.TrimRight(baseURL, "/")).Scan(&id)
	}
	return id, nil
}

func (s *Store) SetBackendEnabled(id int64, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	res, err := s.DB.Exec(`UPDATE backends SET enabled=? WHERE id=?`, en, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteBackend(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM backends WHERE id=?`, id)
	return err
}

func (s *Store) ListModels() ([]Model, error) {
	rows, err := s.DB.Query(`SELECT id, alias, upstream_name, lb_policy, enabled, max_context, fallback, prompt_cache, media, hub_share, hub_node_id, hub_node_name FROM models ORDER BY alias`)
	if err != nil {
		return nil, err
	}
	var out []Model
	for rows.Next() {
		m, err := scanModelRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	qerr := rows.Err()
	rows.Close()
	if qerr != nil {
		return nil, qerr
	}
	// Second query after Close: MaxOpenConns=1 deadlocks if rows stay open.
	for i := range out {
		ids, err := s.modelBackendIDs(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].BackendIDs = ids
	}
	return out, nil
}

func (s *Store) GetModel(id int64) (Model, error) {
	m, err := scanModelRow(s.DB.QueryRow(`SELECT id, alias, upstream_name, lb_policy, enabled, max_context, fallback, prompt_cache, media, hub_share, hub_node_id, hub_node_name FROM models WHERE id=?`, id))
	if err != nil {
		return m, err
	}
	m.BackendIDs, err = s.modelBackendIDs(m.ID)
	return m, err
}

func (s *Store) GetModelByAlias(alias string) (Model, error) {
	m, err := scanModelRow(s.DB.QueryRow(`SELECT id, alias, upstream_name, lb_policy, enabled, max_context, fallback, prompt_cache, media, hub_share, hub_node_id, hub_node_name FROM models WHERE alias=?`, alias))
	if err != nil {
		return m, err
	}
	m.BackendIDs, err = s.modelBackendIDs(m.ID)
	return m, err
}

func scanModelRow(r rowScanner) (Model, error) {
	var m Model
	var en, share int
	var media string
	if err := r.Scan(&m.ID, &m.Alias, &m.UpstreamName, &m.LBPolicy, &en, &m.MaxContext, &m.Fallback, &m.PromptCache, &media, &share, &m.HubNodeID, &m.HubNodeName); err != nil {
		return m, err
	}
	m.Enabled = en == 1
	m.HubShare = share == 1
	m.Media = domain.ParseMedia(media)
	return m, nil
}

func (s *Store) modelBackendIDs(modelID int64) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT backend_id FROM model_backends WHERE model_id=?`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) SaveModel(m Model) (int64, error) {
	if m.LBPolicy == "" {
		m.LBPolicy = "least_conn"
	}
	m.PromptCache = strings.ToLower(strings.TrimSpace(m.PromptCache))
	switch m.PromptCache {
	case "inherit", "off", "auto", "on":
	default:
		m.PromptCache = "inherit"
	}
	en := 0
	if m.Enabled {
		en = 1
	}
	share := 0
	if m.HubShare {
		share = 1
	}
	if m.UpstreamName == "" {
		m.UpstreamName = m.Alias
	}
	m.Fallback = strings.TrimSpace(m.Fallback)
	if m.Fallback == m.Alias {
		m.Fallback = ""
	}
	taken, err := s.AliasTaken(m.Alias, 0, m.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, fmt.Errorf("alias %s already used", m.Alias)
	}
	if m.ID == 0 {
		if m.HubNodeID != "" {
			m.HubShare = false
			share = 0
		}
		res, err := s.DB.Exec(`INSERT INTO models (alias, upstream_name, lb_policy, enabled, max_context, fallback, prompt_cache, media, hub_share, hub_node_id, hub_node_name) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			m.Alias, m.UpstreamName, m.LBPolicy, en, m.MaxContext, m.Fallback, m.PromptCache, domain.JoinMedia(m.Media), share, m.HubNodeID, m.HubNodeName)
		if err != nil {
			return 0, err
		}
		id, _ := res.LastInsertId()
		m.ID = id
	} else {
		if m.HubNodeID != "" {
			m.HubShare = false
			share = 0
		}
		_, err := s.DB.Exec(`UPDATE models SET alias=?, upstream_name=?, lb_policy=?, enabled=?, max_context=?, fallback=?, prompt_cache=?, media=?, hub_share=?, hub_node_id=?, hub_node_name=? WHERE id=?`,
			m.Alias, m.UpstreamName, m.LBPolicy, en, m.MaxContext, m.Fallback, m.PromptCache, domain.JoinMedia(m.Media), share, m.HubNodeID, m.HubNodeName, m.ID)
		if err != nil {
			return 0, err
		}
	}
	if _, err := s.DB.Exec(`DELETE FROM model_backends WHERE model_id=?`, m.ID); err != nil {
		return 0, err
	}
	for _, bid := range m.BackendIDs {
		if _, err := s.DB.Exec(`INSERT INTO model_backends (model_id, backend_id) VALUES (?,?)`, m.ID, bid); err != nil {
			return 0, err
		}
	}
	return m.ID, nil
}

func (s *Store) DeleteModel(id int64) error {
	var alias string
	_ = s.DB.QueryRow(`SELECT alias FROM models WHERE id=?`, id).Scan(&alias)
	if alias != "" {
		_, _ = s.DB.Exec(`UPDATE models SET fallback='' WHERE fallback=?`, alias)
	}
	_, err := s.DB.Exec(`DELETE FROM models WHERE id=?`, id)
	return err
}

// ConnectOllamaModel creates or updates an alias named after the Ollama model.
func (s *Store) ConnectOllamaModel(name string, backendIDs []int64, policy string, maxContext int) error {
	name = strings.TrimSpace(name)
	if name == "" || len(backendIDs) == 0 {
		return fmt.Errorf("model and backends required")
	}
	if policy == "" {
		policy = "least_conn"
	}
	m, err := s.GetModelByAlias(name)
	if err != nil {
		m = Model{Alias: name, UpstreamName: name, LBPolicy: policy, Enabled: true, BackendIDs: backendIDs, MaxContext: maxContext, Media: domain.InferMedia(name, nil)}
	} else {
		m.BackendIDs = backendIDs
		m.Enabled = true
		if m.UpstreamName == "" {
			m.UpstreamName = name
		}
		if m.MaxContext == 0 && maxContext > 0 {
			m.MaxContext = maxContext
		}
	}
	_, err = s.SaveModel(m)
	return err
}

func (s *Store) ConnectHubModel(alias, nodeID, nodeName string, maxContext int, media []string) error {
	alias = strings.TrimSpace(alias)
	nodeID = strings.TrimSpace(nodeID)
	nodeName = strings.TrimSpace(nodeName)
	if alias == "" || nodeID == "" {
		return fmt.Errorf("hub model and node required")
	}
	if nodeName == "" {
		nodeName = nodeID
		if len(nodeName) > 10 {
			nodeName = nodeName[:10]
		}
	}
	want := alias
	if existing, err := s.GetModelByAlias(want); err == nil {
		if existing.HubNodeID == nodeID {
			existing.Enabled = true
			existing.UpstreamName = alias
			existing.HubNodeName = nodeName
			if maxContext > 0 {
				existing.MaxContext = maxContext
			}
			if len(media) > 0 {
				existing.Media = media
			}
			existing.HubShare = false
			_, err = s.SaveModel(existing)
			return err
		}
		want = alias + "@" + nodeName
	}
	m := Model{
		Alias: want, UpstreamName: alias, LBPolicy: "least_conn", Enabled: true,
		MaxContext: maxContext, Media: media, HubNodeID: nodeID, HubNodeName: nodeName,
	}
	if len(m.Media) == 0 {
		m.Media = domain.InferMedia(alias, nil)
	}
	_, err := s.SaveModel(m)
	return err
}

func (s *Store) UpsertHubAuto(nodeID, nodeName, upstream string, maxContext int, media []string) error {
	nodeID = strings.TrimSpace(nodeID)
	upstream = strings.TrimSpace(upstream)
	if nodeID == "" || upstream == "" {
		return fmt.Errorf("hub auto node and alias required")
	}
	if nodeName == "" {
		nodeName = nodeID
		if len(nodeName) > 10 {
			nodeName = nodeName[:10]
		}
	}
	m, err := s.GetModelByAlias(domain.HubAutoAlias)
	if err != nil {
		m = Model{Alias: domain.HubAutoAlias, LBPolicy: "least_conn"}
	}
	m.Alias = domain.HubAutoAlias
	m.UpstreamName = upstream
	m.Enabled = true
	m.HubShare = false
	m.HubNodeID = nodeID
	m.HubNodeName = nodeName
	m.BackendIDs = nil
	if maxContext > 0 {
		m.MaxContext = maxContext
	}
	if len(media) > 0 {
		m.Media = media
	} else if len(m.Media) == 0 {
		m.Media = domain.InferMedia(upstream, nil)
	}
	_, err = s.SaveModel(m)
	return err
}

func (s *Store) DeleteHubAuto() error {
	m, err := s.GetModelByAlias(domain.HubAutoAlias)
	if err != nil {
		return nil
	}
	if m.HubNodeID == "" {
		return nil
	}
	return s.DeleteModel(m.ID)
}

func (s *Store) ListKeys() ([]APIKey, error) {
	rows, err := s.DB.Query(`SELECT id, name, prefix, key_hash, allowed_models, rpm, enabled, created_at, last_used_at, request_count FROM api_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanKey(r rowScanner) (APIKey, error) {
	var k APIKey
	var allowed, created string
	var last sql.NullString
	var en int
	if err := r.Scan(&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &allowed, &k.RPM, &en, &created, &last, &k.RequestCount); err != nil {
		return k, err
	}
	k.Enabled = en == 1
	_ = json.Unmarshal([]byte(allowed), &k.AllowedModels)
	k.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if last.Valid {
		t, _ := time.Parse(time.RFC3339, last.String)
		k.LastUsedAt = &t
	}
	return k, nil
}

func (s *Store) GetKeyByHash(hash string) (APIKey, error) {
	row := s.DB.QueryRow(`SELECT id, name, prefix, key_hash, allowed_models, rpm, enabled, created_at, last_used_at, request_count FROM api_keys WHERE key_hash=?`, hash)
	return scanKey(row)
}

func (s *Store) GetKey(id int64) (APIKey, error) {
	row := s.DB.QueryRow(`SELECT id, name, prefix, key_hash, allowed_models, rpm, enabled, created_at, last_used_at, request_count FROM api_keys WHERE id=?`, id)
	return scanKey(row)
}

func (s *Store) InsertKey(k APIKey) (int64, error) {
	raw, _ := json.Marshal(k.AllowedModels)
	en := 0
	if k.Enabled {
		en = 1
	}
	res, err := s.DB.Exec(`INSERT INTO api_keys (name, prefix, key_hash, allowed_models, rpm, enabled, created_at) VALUES (?,?,?,?,?,?,?)`,
		k.Name, k.Prefix, k.KeyHash, string(raw), k.RPM, en, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateKey(k APIKey) error {
	raw, _ := json.Marshal(k.AllowedModels)
	en := 0
	if k.Enabled {
		en = 1
	}
	_, err := s.DB.Exec(`UPDATE api_keys SET name=?, allowed_models=?, rpm=?, enabled=? WHERE id=?`,
		k.Name, string(raw), k.RPM, en, k.ID)
	return err
}

func (s *Store) DeleteKey(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM api_keys WHERE id=?`, id)
	return err
}

func (s *Store) TouchKey(id int64) {
	_, _ = s.DB.Exec(`UPDATE api_keys SET last_used_at=?, request_count=request_count+1 WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
}

func (s *Store) PromptCacheMode() string {
	var v string
	_ = s.DB.QueryRow(`SELECT prompt_cache FROM admin_meta WHERE id=1`).Scan(&v)
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "off", "auto", "on":
		return v
	default:
		return "auto"
	}
}

func (s *Store) SetPromptCacheMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "off", "auto", "on":
	default:
		return fmt.Errorf("prompt_cache must be off, auto or on")
	}
	_, err := s.DB.Exec(`UPDATE admin_meta SET prompt_cache=? WHERE id=1`, mode)
	return err
}

func (s *Store) HubSettings() (domain.HubSettings, error) {
	var en int
	var id, tok, name, sched string
	err := s.DB.QueryRow(`SELECT hub_enabled, hub_node_id, hub_token, hub_name, hub_schedule FROM admin_meta WHERE id=1`).Scan(&en, &id, &tok, &name, &sched)
	if err == sql.ErrNoRows {
		return domain.HubSettings{}, nil
	}
	if err != nil {
		return domain.HubSettings{}, err
	}
	h := domain.HubSettings{Enabled: en == 1, NodeID: id, Token: tok, Name: name}
	if strings.TrimSpace(sched) != "" {
		_ = json.Unmarshal([]byte(sched), &h.Schedule)
	}
	return h, nil
}

func (s *Store) SetHubSettings(h domain.HubSettings) error {
	en := 0
	if h.Enabled {
		en = 1
	}
	raw, _ := json.Marshal(h.Schedule)
	res, err := s.DB.Exec(`UPDATE admin_meta SET hub_enabled=?, hub_node_id=?, hub_token=?, hub_name=?, hub_schedule=? WHERE id=1`,
		en, h.NodeID, h.Token, strings.TrimSpace(h.Name), string(raw))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin_meta missing")
	}
	return nil
}

func (s *Store) Log(prefix, model, backend string, status int, latency time.Duration, bytesOut int64, u domain.TokenUsage) {
	cost, saved, disc := 0.0, 0.0, 0.0
	if u.HasCost {
		cost = u.Cost
	}
	if u.HasSaved {
		saved = u.SavedUSD
	}
	if u.HasDiscount {
		disc = u.CacheDiscount
	} else {
		disc = u.CacheDiscount
	}
	now := time.Now().UTC()
	_, _ = s.DB.Exec(`INSERT INTO request_log (ts, key_prefix, model, backend, status, latency_ms, bytes_out, prompt_tokens, completion_tokens, cached_tokens, cache_write_tokens, upstream, prompt_usd, cache_discount, usage_cost, saved_usd) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		now.Format(time.RFC3339Nano), prefix, model, backend, status, latency.Milliseconds(), bytesOut,
		u.PromptTokens, u.CompletionTokens, u.CachedTokens, u.CacheWriteTokens, u.Upstream, u.PromptUSD, disc, cost, saved)
	_, _ = s.DB.Exec(`DELETE FROM request_log WHERE id NOT IN (SELECT id FROM request_log ORDER BY id DESC LIMIT 500)`)
	s.addBilling(now, 1, u.PromptTokens, u.CompletionTokens, u.CachedTokens, cost, saved)
}

func (s *Store) ListLogs(limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id, ts, key_prefix, model, backend, status, latency_ms, bytes_out, prompt_tokens, completion_tokens, cached_tokens, cache_write_tokens, upstream, prompt_usd, cache_discount, usage_cost, saved_usd FROM request_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestLog
	for rows.Next() {
		var l RequestLog
		var ts string
		if err := rows.Scan(&l.ID, &ts, &l.KeyPrefix, &l.Model, &l.Backend, &l.Status, &l.LatencyMS, &l.BytesOut,
			&l.PromptTokens, &l.CompletionTokens, &l.CachedTokens, &l.CacheWriteTokens, &l.Upstream, &l.PromptUSD, &l.CacheDiscount, &l.UsageCost, &l.SavedUSD); err != nil {
			return nil, err
		}
		l.TS, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) UpsertJob(j domain.Job) error {
	_, err := s.DB.Exec(`
INSERT INTO ollama_jobs (id, kind, backend_id, backend, model, status, percent, message, error, log, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  kind=excluded.kind, backend_id=excluded.backend_id, backend=excluded.backend,
  model=excluded.model, status=excluded.status, percent=excluded.percent,
  message=excluded.message, error=excluded.error, log=excluded.log, updated_at=excluded.updated_at
`, j.ID, j.Kind, j.BackendID, j.Backend, j.Model, j.Status, j.Percent, j.Message, j.Error, j.Log, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListJobs() ([]domain.Job, error) {
	rows, err := s.DB.Query(`
SELECT id, kind, backend_id, backend, model, status, percent, message, error, log
FROM ollama_jobs
ORDER BY CASE status WHEN 'running' THEN 0 ELSE 1 END, updated_at DESC
LIMIT 12`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Job
	for rows.Next() {
		var j domain.Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.BackendID, &j.Backend, &j.Model, &j.Status, &j.Percent, &j.Message, &j.Error, &j.Log); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) SeedIfEmpty(backends []Backend) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM backends`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, b := range backends {
		if _, err := s.UpsertBackend(b.Name, b.BaseURL, true, b.Weight, b.Kind, b.Token); err != nil {
			return fmt.Errorf("seed backend %s: %w", b.Name, err)
		}
	}
	return nil
}
