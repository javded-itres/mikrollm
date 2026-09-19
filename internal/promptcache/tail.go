package promptcache

import (
	"bytes"
	"encoding/json"
)

const TailMax = 64 << 10

type Scanner struct {
	frag      []byte
	lastUsage []byte
}

func (s *Scanner) LastUsage() []byte { return s.lastUsage }

func (s *Scanner) Feed(p []byte) {
	if len(p) == 0 {
		return
	}
	s.frag = append(s.frag, p...)
	if len(s.frag) > TailMax {
		s.frag = nil
		return
	}
	for {
		i := bytes.IndexByte(s.frag, '\n')
		if i < 0 {
			if len(s.frag) > TailMax {
				s.frag = nil
			}
			return
		}
		line := bytes.TrimRight(s.frag[:i], "\r")
		s.frag = append([]byte(nil), s.frag[i+1:]...)
		s.line(line)
	}
}

func (s *Scanner) line(line []byte) {
	if len(line) == 0 {
		return
	}
	if line[0] == ':' {
		return
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		payload := bytes.TrimSpace(line[5:])
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			return
		}
		s.consider(payload)
		return
	}
	if line[0] == '{' {
		s.consider(line)
	}
}

func (s *Scanner) consider(payload []byte) {
	if len(payload) > TailMax {
		return
	}
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return
	}
	if _, ok := m["usage"]; ok {
		s.lastUsage = append([]byte(nil), payload...)
		return
	}
	if done, _ := m["done"].(bool); done {
		s.lastUsage = append([]byte(nil), payload...)
		return
	}
	if _, ok := m["prompt_eval_count"]; ok {
		s.lastUsage = append([]byte(nil), payload...)
	}
}
