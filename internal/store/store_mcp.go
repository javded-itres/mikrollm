package store

import (
	"database/sql"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func scanMCP(row interface{ Scan(...any) error }) (domain.MCPUpstream, error) {
	var u domain.MCPUpstream
	var en, share int
	err := row.Scan(&u.ID, &u.Name, &u.URL, &u.Token, &en, &share)
	u.Enabled = en == 1
	u.HubShare = share == 1
	return u, err
}

func (s *Store) ListMCP() ([]domain.MCPUpstream, error) {
	rows, err := s.DB.Query(`SELECT id, name, url, token, enabled, hub_share FROM mcp_upstreams ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MCPUpstream
	for rows.Next() {
		u, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListSharedMCP() ([]domain.MCPUpstream, error) {
	rows, err := s.DB.Query(`SELECT id, name, url, token, enabled, hub_share FROM mcp_upstreams WHERE enabled=1 AND hub_share=1 ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MCPUpstream
	for rows.Next() {
		u, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GetMCP(id int64) (domain.MCPUpstream, error) {
	return scanMCP(s.DB.QueryRow(`SELECT id, name, url, token, enabled, hub_share FROM mcp_upstreams WHERE id=?`, id))
}

func (s *Store) GetMCPByName(name string) (domain.MCPUpstream, error) {
	name, err := domain.NormalizeMCPName(name)
	if err != nil {
		return domain.MCPUpstream{}, err
	}
	return scanMCP(s.DB.QueryRow(`SELECT id, name, url, token, enabled, hub_share FROM mcp_upstreams WHERE name=?`, name))
}

func (s *Store) MCPUpstream(name string) (domain.MCPUpstream, error) {
	return s.GetMCPByName(name)
}

func (s *Store) SaveMCP(u domain.MCPUpstream) (int64, error) {
	name, err := domain.NormalizeMCPName(u.Name)
	if err != nil {
		return 0, err
	}
	rawURL := strings.TrimSpace(u.URL)
	if !domain.ValidMCPURL(rawURL) {
		return 0, errMCPURL
	}
	if u.ID > 0 && u.Token == "" {
		old, err := s.GetMCP(u.ID)
		if err != nil {
			return 0, err
		}
		u.Token = old.Token
	}
	en, share := 0, 0
	if u.Enabled {
		en = 1
	}
	if u.HubShare {
		share = 1
	}
	if u.ID == 0 {
		res, err := s.DB.Exec(`INSERT INTO mcp_upstreams (name, url, token, enabled, hub_share) VALUES (?,?,?,?,?)`,
			name, rawURL, u.Token, en, share)
		if err != nil {
			return 0, mapMCPErr(err)
		}
		return res.LastInsertId()
	}
	res, err := s.DB.Exec(`UPDATE mcp_upstreams SET name=?, url=?, token=?, enabled=?, hub_share=? WHERE id=?`,
		name, rawURL, u.Token, en, share, u.ID)
	if err != nil {
		return 0, mapMCPErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, sql.ErrNoRows
	}
	return u.ID, nil
}

func (s *Store) DeleteMCP(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM mcp_upstreams WHERE id=?`, id)
	return err
}

type mcpURLError struct{}

func (mcpURLError) Error() string { return "mcp url" }

var errMCPURL error = mcpURLError{}

func mapMCPErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return errMCPNameTaken
	}
	return err
}

type mcpNameTaken struct{}

func (mcpNameTaken) Error() string { return "mcp name taken" }

var errMCPNameTaken error = mcpNameTaken{}
