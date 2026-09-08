package health

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

// Ollama Cloud publishes $/1M on https://ollama.com/pricing and on each
// /library/<slug> page. /api/tags has no prices, and tag names often differ
// from the table (gemma4:31b vs gemma4).
const (
	ollamaPricingTTL      = 5 * time.Minute
	ollamaLibraryFetchCap = 4
	ollamaPricingMaxBody  = 1 << 20
)

type ollamaCloudPrice struct {
	Name   string
	Slug   string
	Input  float64
	Output float64
}

type ollamaPriceIndex struct {
	exact  map[string]ollamaCloudPrice
	family map[string]ollamaCloudPrice
}

type ollamaPriceSnap struct {
	at    time.Time
	index ollamaPriceIndex
}

type ollamaLibSnap struct {
	at time.Time
	p  ollamaCloudPrice
	ok bool
}

var (
	reHTMLTable      = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
	reHTMLRow        = regexp.MustCompile(`(?is)<tr\b[^>]*>.*?</tr>`)
	reHTMLCell       = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>.*?</t[dh]>`)
	reHTMLTags       = regexp.MustCompile(`(?s)<[^>]+>`)
	reHTMLSpace      = regexp.MustCompile(`\s+`)
	reLibraryHref    = regexp.MustCompile(`(?i)href=["']/library/([^"'?#]+)`)
	reLibraryRate    = regexp.MustCompile(`(?is)data-rate="(base|peak)"[^>]*>\s*\$([0-9][0-9,]*(?:\.[0-9]+)?)\s*</div>\s*<div[^>]*>\s*(input|cached|output)\b`)
	reLibraryRateAny = regexp.MustCompile(`(?is)\$([0-9][0-9,]*(?:\.[0-9]+)?)\s*</div>\s*<div[^>]*>\s*(input|cached|output)\b`)
)

func (c *Checker) applyOllamaCloudPrices(b domain.Backend, st *Status) {
	if st == nil || len(st.Models) == 0 {
		return
	}
	if st.Prompt == nil {
		st.Prompt = map[string]float64{}
	}
	if st.Completion == nil {
		st.Completion = map[string]float64{}
	}
	if st.Priced == nil {
		st.Priced = map[string]bool{}
	}
	idx := c.ollamaPricingIndex(b)
	fetched := 0
	for _, name := range st.Models {
		if p, ok := idx.lookup(name); ok {
			st.Priced[name] = true
			st.Prompt[name] = p.Input
			st.Completion[name] = p.Output
			continue
		}
		if fetched >= ollamaLibraryFetchCap {
			continue
		}
		slug := ollamaLibrarySlug(name)
		if slug == "" {
			continue
		}
		fetched++
		p, ok := c.ollamaLibraryPrice(b, slug)
		if !ok {
			continue
		}
		st.Priced[name] = true
		st.Prompt[name] = p.Input
		st.Completion[name] = p.Output
	}
}

func (c *Checker) ollamaPricingIndex(b domain.Backend) ollamaPriceIndex {
	c.ocMu.Lock()
	fresh := !c.ocPrices.at.IsZero() && time.Since(c.ocPrices.at) < ollamaPricingTTL && len(c.ocPrices.index.exact) > 0
	snap := c.ocPrices
	c.ocMu.Unlock()
	if fresh {
		return snap.index
	}
	idx, err := c.fetchOllamaPricing(b)
	if err != nil || len(idx.exact) == 0 {
		if len(snap.index.exact) > 0 {
			return snap.index
		}
		return idx
	}
	c.ocMu.Lock()
	c.ocPrices = ollamaPriceSnap{at: time.Now(), index: idx}
	c.ocMu.Unlock()
	return idx
}

func (c *Checker) fetchOllamaPricing(b domain.Backend) (ollamaPriceIndex, error) {
	resp, err := c.doGET(b, "/pricing")
	if err != nil {
		return ollamaPriceIndex{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ollamaPriceIndex{}, fmt.Errorf("ollama pricing %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, ollamaPricingMaxBody))
	if err != nil {
		return ollamaPriceIndex{}, err
	}
	return indexOllamaPrices(parseOllamaPricingHTML(string(raw))), nil
}

func (c *Checker) ollamaLibraryPrice(b domain.Backend, slug string) (ollamaCloudPrice, bool) {
	c.ocMu.Lock()
	if s, ok := c.ocLib[slug]; ok && time.Since(s.at) < ollamaPricingTTL {
		c.ocMu.Unlock()
		return s.p, s.ok
	}
	c.ocMu.Unlock()
	p, ok := c.fetchOllamaLibraryPrice(b, slug)
	c.ocMu.Lock()
	if c.ocLib == nil {
		c.ocLib = map[string]ollamaLibSnap{}
	}
	c.ocLib[slug] = ollamaLibSnap{at: time.Now(), p: p, ok: ok}
	c.ocMu.Unlock()
	return p, ok
}

func (c *Checker) fetchOllamaLibraryPrice(b domain.Backend, slug string) (ollamaCloudPrice, bool) {
	resp, err := c.doGET(b, "/library/"+slug)
	if err != nil {
		return ollamaCloudPrice{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ollamaCloudPrice{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, ollamaPricingMaxBody))
	if err != nil {
		return ollamaCloudPrice{}, false
	}
	in, out, ok := parseOllamaLibraryHTML(string(raw))
	if !ok {
		return ollamaCloudPrice{}, false
	}
	return ollamaCloudPrice{Name: slug, Slug: slug, Input: in, Output: out}, true
}

func parseOllamaPricingHTML(raw string) []ollamaCloudPrice {
	section := raw
	low := strings.ToLower(raw)
	if i := strings.Index(low, `id="model-pricing"`); i >= 0 {
		section = raw[i:]
		low = strings.ToLower(section)
	}
	if j := strings.Index(low, "peak pricing"); j > 0 {
		section = section[:j]
	}
	table := reHTMLTable.FindString(section)
	if table == "" {
		table = reHTMLTable.FindString(raw)
	}
	if table == "" {
		return nil
	}
	var out []ollamaCloudPrice
	for _, row := range reHTMLRow.FindAllString(table, -1) {
		cells := reHTMLCell.FindAllString(row, -1)
		if len(cells) < 4 {
			continue
		}
		name := cellText(cells[0])
		if name == "" || strings.EqualFold(name, "model") {
			continue
		}
		in, inOK := parseOllamaMoney(cellText(cells[1]))
		outp, outOK := parseOllamaMoney(cellText(cells[3]))
		if !inOK && !outOK {
			continue
		}
		slug := ""
		if m := reLibraryHref.FindStringSubmatch(cells[0]); len(m) == 2 {
			slug = strings.ToLower(strings.Trim(m[1], "/"))
		}
		out = append(out, ollamaCloudPrice{Name: name, Slug: slug, Input: in, Output: outp})
	}
	return out
}

func parseOllamaLibraryHTML(raw string) (input, output float64, ok bool) {
	var inOK, outOK bool
	matches := reLibraryRate.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		matches = reLibraryRateAny.FindAllStringSubmatch(raw, -1)
	}
	for _, m := range matches {
		rate := "base"
		amount, label := "", ""
		switch len(m) {
		case 4:
			rate, amount, label = strings.ToLower(m[1]), m[2], strings.ToLower(m[3])
		case 3:
			amount, label = m[1], strings.ToLower(m[2])
		default:
			continue
		}
		if rate == "peak" {
			continue
		}
		n, parsed := parseOllamaMoney("$" + amount)
		if !parsed {
			continue
		}
		switch label {
		case "input":
			input, inOK = n, true
		case "output":
			output, outOK = n, true
		}
	}
	return input, output, inOK || outOK
}

func indexOllamaPrices(rows []ollamaCloudPrice) ollamaPriceIndex {
	idx := ollamaPriceIndex{
		exact:  map[string]ollamaCloudPrice{},
		family: map[string]ollamaCloudPrice{},
	}
	familyCount := map[string]int{}
	slugCount := map[string]int{}
	for _, p := range rows {
		key := ollamaBare(p.Name)
		if key == "" {
			continue
		}
		idx.exact[key] = p
		if fam := ollamaFamily(p.Name); fam != "" {
			familyCount[fam]++
		}
		if p.Slug != "" {
			slugCount[p.Slug]++
		}
	}
	for _, p := range rows {
		if p.Slug == "" || slugCount[p.Slug] != 1 {
			continue
		}
		if _, exists := idx.exact[p.Slug]; exists {
			continue
		}
		idx.exact[p.Slug] = p
	}
	for _, p := range rows {
		fam := ollamaFamily(p.Name)
		if fam == "" || familyCount[fam] != 1 {
			continue
		}
		idx.family[fam] = p
	}
	return idx
}

func (idx ollamaPriceIndex) lookup(name string) (ollamaCloudPrice, bool) {
	key := ollamaBare(name)
	if key == "" {
		return ollamaCloudPrice{}, false
	}
	if p, ok := idx.exact[key]; ok {
		return p, true
	}
	if p, ok := idx.family[ollamaFamily(name)]; ok {
		return p, true
	}
	return ollamaCloudPrice{}, false
}

func ollamaBare(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimSuffix(n, ":cloud")
	n = strings.TrimSuffix(n, "-cloud")
	return n
}

func ollamaFamily(name string) string {
	n := ollamaBare(name)
	if i := strings.Index(n, ":"); i > 0 {
		return n[:i]
	}
	return n
}

func ollamaLibrarySlug(name string) string {
	return ollamaFamily(name)
}

func cellText(raw string) string {
	s := reHTMLTags.ReplaceAllString(raw, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(reHTMLSpace.ReplaceAllString(s, " "))
}

func parseOllamaMoney(s string) (float64, bool) {
	s = strings.TrimSpace(html.UnescapeString(s))
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" || s == "-" || strings.EqualFold(s, "n/a") {
		return 0, false
	}
	s = strings.TrimPrefix(s, "$")
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
