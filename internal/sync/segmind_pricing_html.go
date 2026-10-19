package sync

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/registry"
)

var (
	segTable  = regexp.MustCompile(`(?s)<table.*?</table>`)
	segRow    = regexp.MustCompile(`(?s)<tr.*?</tr>`)
	segCell   = regexp.MustCompile(`(?s)<t[hd][^>]*>(.*?)</t[hd]>`)
	segTag    = regexp.MustCompile(`<[^>]+>`)
	segDollar = regexp.MustCompile(`\$\s*([0-9][0-9,]*\.?[0-9]*)`)
	segPerSec = regexp.MustCompile(`(?i)\$\s*([0-9.]+)\s*/\s*per (gpu )?second`)
	segAbout  = regexp.MustCompile(`(?i)about \$([0-9.]+) per second at (\d+p)`)
)

// segPage is a pricing page reduced to the facts the price model can use.
type segPage struct {
	tables [][][]string
	text   string
}

func readSegPage(page []byte) segPage {
	var sp segPage
	for _, t := range segTable.FindAllString(string(page), -1) {
		var rows [][]string
		for _, r := range segRow.FindAllString(t, -1) {
			var cells []string
			for _, c := range segCell.FindAllStringSubmatch(r, -1) {
				cells = append(cells, strings.TrimSpace(html.UnescapeString(segTag.ReplaceAllString(c[1], ""))))
			}
			rows = append(rows, cells)
		}
		if len(rows) > 1 {
			sp.tables = append(sp.tables, rows)
		}
	}
	sp.text = html.UnescapeString(segTag.ReplaceAllString(string(page), " "))
	return sp
}

// parseSegmindHTML recognises the table shapes Segmind renders on its pricing
// pages. It is the fallback when a page carries no pricingData object, and the
// preferred reading for token-priced pages, whose tables hold exact per-second
// or per-quality figures. avg is the catalog price, kept for token-only pages.
func parseSegmindHTML(page []byte, avg registry.Price) (registry.Price, bool) {
	sp := readSegPage(page)
	p := registry.Price{Source: registry.SourcePage}
	var secTiers, tokTiers []registry.PriceTier
	var notes []string
	aspectTables := 0
	for _, t := range sp.tables {
		head := strings.ToLower(strings.Join(t[0], "|"))
		switch {
		case strings.HasPrefix(head, "input type|resolution|rate"):
			for _, r := range t[1:] {
				if v, ok := dollars(cell(r, 2)); ok && len(r) >= 3 {
					video := strings.Contains(strings.ToLower(r[0]), "video")
					tokTiers = append(tokTiers, tier(res(r[1]), video, v, ""))
				}
			}
		case strings.HasPrefix(head, "resolution|") && strings.Contains(head, "16:9"):
			// Per-second rates by aspect ratio; the first such table is text or
			// image input and the second is video input. 16:9 is the reference.
			col := indexOf(t[0], "16:9")
			for _, r := range t[1:] {
				if v, ok := dollars(cell(r, col)); ok {
					secTiers = append(secTiers, tier(res(r[0]), aspectTables > 0, v, registry.UnitSecond))
				}
			}
			aspectTables++
		case strings.Contains(head, "video cost per second"):
			col := indexOf(t[0], "Video cost per second")
			for _, r := range t[1:] {
				if v, ok := dollars(cell(r, col)); ok {
					secTiers = append(secTiers, registry.PriceTier{Match: map[string]string{"resolution": res(r[0])}, ValueUSD: v})
				}
			}
		case strings.HasPrefix(head, "max height|rate per second"):
			return topazPrice(t, p), true
		case strings.Contains(head, "rate per input minute"):
			return veedPrice(t, p), true
		case strings.Contains(head, "duration") && strings.Contains(head, "cost") && strings.Contains(head, "resolution"):
			secTiers = append(secTiers, perClipToSecond(t)...)
		case strings.HasPrefix(head, "size|low quality"):
			return gptImagePrice(t, p)
		case strings.HasPrefix(head, "token type|price"):
			for _, r := range t[1:] {
				if strings.Contains(strings.ToLower(cell(r, 0)), "output") {
					notes = append(notes, cell(r, 0)+" "+cell(r, 1))
				}
			}
		}
	}
	switch {
	case len(tokTiers) > 0:
		// Seedance: billed on video tokens. Exact per-second tiers from the
		// page win where present; the token formula covers everything else.
		p.Unit, p.Formula = registry.UnitMTokens, registry.FormulaSeedTok
		p.ValueUSD = registry.F(tokTiers[0].ValueUSD)
		p.Tiers = secTiers
		if len(secTiers) == 0 {
			p.Tiers = tokTiers
		}
		p.Note = "video tokens; with video input Segmind bills reference + output seconds (min 2/3 of output), estimate counts output only"
		return p, true
	case len(secTiers) > 0:
		p.Unit, p.Tiers = registry.UnitSecond, secTiers
		p.ValueUSD = registry.F(defaultTier(secTiers))
		p.Note = "default is the 720p rate"
		return p, true
	}
	if m := segPerSec.FindStringSubmatch(sp.text); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		if m[2] == "" {
			p.Unit, p.ValueUSD = registry.UnitSecond, registry.F(v)
			return p, true
		}
		notes = append(notes, fmt.Sprintf("billed $%g per GPU second", v))
	}
	if m := segAbout.FindStringSubmatch(sp.text); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		p.Unit, p.ValueUSD = registry.UnitSecond, registry.F(v)
		p.Note = "approximate rate at " + m[2] + "; billed on tokens"
		return p, true
	}
	if len(notes) > 0 && avg.ValueUSD != nil {
		avg.Note = "token-priced (" + strings.Join(notes, "; ") + "); per-request average shown"
		return avg, true
	}
	return registry.Price{}, false
}

func topazPrice(t [][]string, p registry.Price) registry.Price {
	p.Unit = registry.UnitSecond
	for _, r := range t[1:] {
		v, ok := dollars(cell(r, 1))
		if !ok {
			continue
		}
		switch strings.TrimSpace(r[0]) {
		case "720":
			for _, rs := range []string{"360p", "480p", "540p", "720p"} {
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": rs}, ValueUSD: v})
			}
		case "1080":
			p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": "1080p"}, ValueUSD: v})
		default:
			p.ValueUSD = registry.F(v) // everything above 1080p
		}
	}
	p.Note = "rate by output max height; resolution is the upscale target"
	return p
}

func veedPrice(t [][]string, p registry.Price) registry.Price {
	p.Unit = registry.UnitMinute
	var rows []string
	for _, r := range t[1:] {
		v, ok := dollars(cell(r, 2))
		if !ok {
			continue
		}
		rows = append(rows, fmt.Sprintf("%s %s $%g", r[0], r[1], v))
		basic := strings.EqualFold(r[0], "basic")
		switch {
		case basic && strings.Contains(strings.ToLower(r[1]), "up to") && p.ValueUSD == nil:
			p.ValueUSD = registry.F(v)
		case basic && strings.Contains(strings.ToLower(r[1]), "above"):
			for _, rs := range []string{"1440p", "2k", "4k"} {
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": rs}, ValueUSD: v})
			}
		}
	}
	p.Note = "per input minute, Basic preset; " + strings.Join(rows, "; ")
	return p
}

func gptImagePrice(t [][]string, p registry.Price) (registry.Price, bool) {
	for _, r := range t[1:] {
		if strings.ReplaceAll(cell(r, 0), " ", "") != "1024x1024" {
			continue
		}
		p.Unit = registry.UnitImage
		for i, q := range []string{"low", "medium", "high"} {
			if v, ok := dollars(cell(r, i+1)); ok {
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"quality": q}, ValueUSD: v})
				if q == "medium" {
					p.ValueUSD = registry.F(v)
				}
			}
		}
		p.Note = "1024x1024, measured per call by Segmind; default is medium quality"
		return p, p.ValueUSD != nil
	}
	return p, false
}

// perClipToSecond turns a full price grid (video input × aspect × resolution ×
// duration → cost) into per-second tiers, using the first aspect ratio listed.
func perClipToSecond(t [][]string) []registry.PriceTier {
	h := t[0]
	vi, ai, ri, di, ci := indexOf(h, "Video Input"), indexOf(h, "Aspect Ratio"), indexOf(h, "Resolution"), indexOf(h, "Duration"), indexOf(h, "Cost")
	if ri < 0 || di < 0 || ci < 0 {
		return nil
	}
	aspect := cell(t[1], ai)
	seen := map[string]bool{}
	var tiers []registry.PriceTier
	for _, r := range t[1:] {
		if ai >= 0 && cell(r, ai) != aspect {
			continue
		}
		d, err := strconv.ParseFloat(cell(r, di), 64)
		v, ok := dollars(cell(r, ci))
		if err != nil || !ok || d <= 0 {
			continue
		}
		video := strings.EqualFold(cell(r, vi), "yes")
		k := fmt.Sprint(res(r[ri]), video)
		if !seen[k] {
			seen[k] = true
			tiers = append(tiers, tier(res(r[ri]), video, v/d, registry.UnitSecond))
		}
	}
	return tiers
}

func tier(resolution string, video bool, v float64, unit string) registry.PriceTier {
	return registry.PriceTier{
		Match: map[string]string{"resolution": resolution, "video_input": strconv.FormatBool(video)},
		Unit:  unit, ValueUSD: v,
	}
}

func defaultTier(tiers []registry.PriceTier) float64 {
	low := tiers[0].ValueUSD
	for _, t := range tiers {
		if t.Match["resolution"] == "720p" && t.Match["video_input"] != "true" {
			return t.ValueUSD
		}
		low = min(low, t.ValueUSD)
	}
	return low
}

func res(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func dollars(s string) (float64, bool) {
	m := segDollar.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	return v, err == nil
}

func cell(r []string, i int) string {
	if i < 0 || i >= len(r) {
		return ""
	}
	return r[i]
}

func indexOf(h []string, name string) int {
	for i, c := range h {
		if strings.HasPrefix(strings.ToLower(c), strings.ToLower(name)) {
			return i
		}
	}
	return -1
}
