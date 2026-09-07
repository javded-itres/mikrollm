package jobs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
)

type Job = domain.Job

type Tracker struct {
	repo     ports.JobRepo
	mu       sync.Mutex
	jobs     []*Job
	lastSave time.Time
}

func New(repo ports.JobRepo) *Tracker {
	t := &Tracker{repo: repo}
	if repo == nil {
		return t
	}
	list, err := repo.ListJobs()
	if err != nil {
		return t
	}
	for i := range list {
		j := list[i]
		t.jobs = append(t.jobs, &j)
	}
	return t
}

func (t *Tracker) Begin(kind string, backendID int64, backend, model string) (*Job, *Job, error) {
	t.mu.Lock()
	for _, j := range t.jobs {
		if j.Status != "running" {
			continue
		}
		if j.BackendID == backendID {
			cp := *j
			t.mu.Unlock()
			return nil, &cp, ports.ErrBusy
		}
	}
	id := newID()
	j := &Job{
		ID: id, Kind: kind, BackendID: backendID, Backend: backend, Model: model,
		Status: "running", Message: starting(kind),
	}
	t.jobs = append([]*Job{j}, t.jobs...)
	if len(t.jobs) > 12 {
		kept := make([]*Job, 0, 12)
		for _, old := range t.jobs {
			if old.Status == "running" || len(kept) < 12 {
				kept = append(kept, old)
			}
		}
		t.jobs = kept
	}
	cp := *j
	t.mu.Unlock()
	t.save(cp, true)
	return &cp, nil, nil
}

func (t *Tracker) Writer(id string) io.WriteCloser {
	return &Writer{t: t, id: id}
}

func (t *Tracker) Fail(id, msg string) {
	t.mu.Lock()
	j := t.find(id)
	if j == nil {
		t.mu.Unlock()
		return
	}
	j.Status = "error"
	j.Error = msg
	j.Message = msg
	appendLog(j, msg)
	cp := *j
	t.mu.Unlock()
	t.save(cp, true)
}

func (t *Tracker) Done(id string) {
	t.mu.Lock()
	j := t.find(id)
	if j == nil {
		t.mu.Unlock()
		return
	}
	j.Status = "done"
	j.Percent = 100
	if j.Message == "" || j.Message == starting(j.Kind) {
		j.Message = "готово"
	}
	cp := *j
	t.mu.Unlock()
	t.save(cp, true)
}

func (t *Tracker) SetMessage(id, msg string) {
	t.mu.Lock()
	j := t.find(id)
	if j == nil {
		t.mu.Unlock()
		return
	}
	j.Message = msg
	cp := *j
	t.mu.Unlock()
	t.save(cp, false)
}

func (t *Tracker) List() []Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Job, len(t.jobs))
	for i, j := range t.jobs {
		cp := *j
		cp.Log = tailLog(cp.Log, 4)
		out[i] = cp
	}
	return out
}

func (t *Tracker) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, j := range t.jobs {
		if j.Status == "running" {
			return true
		}
	}
	return false
}

func (t *Tracker) RunningPull() *Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, j := range t.jobs {
		if j.Kind == "pull" && j.Status == "running" {
			cp := *j
			cp.Log = tailLog(cp.Log, 4)
			return &cp
		}
	}
	return nil
}

func (t *Tracker) find(id string) *Job {
	for _, j := range t.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

func (t *Tracker) save(j Job, force bool) {
	if t.repo == nil {
		return
	}
	if !force {
		t.mu.Lock()
		ok := time.Since(t.lastSave) >= 400*time.Millisecond
		if ok {
			t.lastSave = time.Now()
		}
		t.mu.Unlock()
		if !ok {
			return
		}
	} else {
		t.mu.Lock()
		t.lastSave = time.Now()
		t.mu.Unlock()
	}
	j.Log = tailLog(j.Log, 4)
	_ = t.repo.UpsertJob(j)
}

type Writer struct {
	t   *Tracker
	id  string
	buf []byte
}

func (w *Writer) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	w.flush(false)
	return len(p), nil
}

func (w *Writer) Close() error {
	w.flush(true)
	w.t.mu.Lock()
	j := w.t.find(w.id)
	if j == nil {
		w.t.mu.Unlock()
		return nil
	}
	cp := *j
	w.t.mu.Unlock()
	w.t.save(cp, true)
	return nil
}

func (w *Writer) flush(all bool) {
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if all && len(w.buf) > 0 {
				line := bytes.TrimSpace(w.buf)
				w.buf = nil
				if len(line) > 0 {
					w.t.applyLine(w.id, line)
				}
			}
			return
		}
		line := bytes.TrimSpace(w.buf[:i])
		w.buf = w.buf[i+1:]
		if len(line) > 0 {
			w.t.applyLine(w.id, line)
		}
	}
}

func (t *Tracker) applyLine(id string, line []byte) {
	t.mu.Lock()
	j := t.find(id)
	if j == nil {
		t.mu.Unlock()
		return
	}
	appendLog(j, string(line))
	var m map[string]any
	if json.Unmarshal(line, &m) != nil {
		j.Message = string(line)
	} else {
		if s, _ := m["status"].(string); s != "" {
			j.Message = s
			if s == "success" {
				j.Percent = 100
			}
		}
		if s, _ := m["error"].(string); s != "" {
			j.Status = "error"
			j.Error = s
			j.Message = s
		}
		total, _ := m["total"].(float64)
		done, _ := m["completed"].(float64)
		if total > 0 {
			pct := int(100 * done / total)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			j.Percent = pct
		}
	}
	cp := *j
	force := j.Status == "error" || j.Percent == 100
	save := force || time.Since(t.lastSave) >= 400*time.Millisecond
	if save {
		t.lastSave = time.Now()
	}
	t.mu.Unlock()
	if save {
		t.save(cp, true)
	}
}

func appendLog(j *Job, line string) {
	j.Log = tailLog(strings.TrimSpace(j.Log+"\n"+line), 4)
}

func tailLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "\n")
	if len(parts) <= n {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[len(parts)-n:], "\n")
}

func starting(kind string) string {
	switch kind {
	case "load":
		return "загрузка в RAM…"
	case "pull":
		return "скачивание…"
	default:
		return "выполняется…"
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func IsBusy(err error) bool {
	return errors.Is(err, ports.ErrBusy)
}
