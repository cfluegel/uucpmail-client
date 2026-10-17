package ui

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2/widget"
	"golang.org/x/net/html"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// RenderHTML wandelt (potenziell bösartiges) HTML in Fyne-RichText-Segmente:
// Tokenizer-basiert (robust gegen kaputtes Markup), harte Tag-Whitelist,
// kein CSS, kein JS, keine Remote-Inhalte. Links werden als reiner Text
// »Linktext (https://url)« ausgegeben – nur http/https-URLs erscheinen.
func RenderHTML(src string) []widget.RichTextSegment {
	r := &htmlRenderer{}
	r.render(src)
	r.flush()
	return r.segments
}

// htmlToText liefert die Textfassung (für Zitate beim Antworten auf HTML-Mails).
func htmlToText(src string) string {
	var sb strings.Builder
	for _, seg := range RenderHTML(src) {
		if ts, ok := seg.(*widget.TextSegment); ok {
			sb.WriteString(ts.Text)
			if ts.Style.Inline {
				continue
			}
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

const maxSegments = 5000

// Container-Tags, deren gesamter Inhalt verworfen wird.
var skipTags = map[string]bool{
	"script": true, "style": true, "head": true, "iframe": true,
	"object": true, "embed": true, "form": true, "svg": true,
	"noscript": true, "template": true, "title": true,
}

// Block-Tags, die einen Absatzumbruch erzeugen.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true,
	"table": true, "tr": true, "header": true, "footer": true,
}

type htmlRenderer struct {
	segments    []widget.RichTextSegment
	line        strings.Builder // aktuelle Zeile (Text wird gesammelt)
	bold        int
	italic      int
	mono        int
	heading     int // 0 = kein Heading, 1-2 = Heading, >=3 = SubHeading
	quote       int
	listDepth   int
	olIndex     []int // Zähler je ol-Ebene (-1 = ul)
	skipDepth   int   // >0: Inhalt wird verworfen
	pre         bool
	truncated   bool
	lineEmpty   bool
	lineStarted bool
	cellSep     bool // nächste td bekommt " | "
	href        string
}

func (r *htmlRenderer) render(src string) {
	tok := html.NewTokenizer(strings.NewReader(src))
	for {
		if len(r.segments) >= maxSegments {
			r.truncated = true
			return
		}
		switch tok.Next() {
		case html.ErrorToken:
			return
		case html.TextToken:
			if r.skipDepth > 0 {
				continue
			}
			r.text(string(tok.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			nameB, hasAttr := tok.TagName()
			name := strings.ToLower(string(nameB))
			attrs := map[string]string{}
			for hasAttr {
				k, v, more := tok.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
				hasAttr = more
			}
			r.open(name, attrs)
		case html.EndTagToken:
			nameB, _ := tok.TagName()
			r.close(strings.ToLower(string(nameB)))
		}
	}
}

func (r *htmlRenderer) open(name string, attrs map[string]string) {
	if skipTags[name] {
		r.skipDepth++
		return
	}
	if r.skipDepth > 0 {
		return
	}
	switch name {
	case "br":
		r.newline()
	case "hr":
		r.newline()
		r.text("――――――――――")
		r.newline()
	case "h1", "h2", "h3", "h4", "h5", "h6":
		r.newline()
		r.heading = int(name[1] - '0')
	case "b", "strong":
		r.flushInline()
		r.bold++
	case "i", "em":
		r.flushInline()
		r.italic++
	case "code", "tt", "kbd":
		r.flushInline()
		r.mono++
	case "pre":
		r.newline()
		r.pre = true
		r.mono++
	case "blockquote":
		r.newline()
		r.quote++
	case "ul":
		r.newline()
		r.listDepth++
		r.olIndex = append(r.olIndex, -1)
	case "ol":
		r.newline()
		r.listDepth++
		r.olIndex = append(r.olIndex, 0)
	case "li":
		r.newline()
		indent := strings.Repeat("  ", max(r.listDepth-1, 0))
		if n := len(r.olIndex); n > 0 && r.olIndex[n-1] >= 0 {
			r.olIndex[n-1]++
			r.rawText(fmt.Sprintf("%s%d. ", indent, r.olIndex[n-1]))
		} else {
			r.rawText(indent + "• ")
		}
	case "a":
		r.href = attrs["href"]
	case "img":
		alt := strings.TrimSpace(attrs["alt"])
		r.text(fmt.Sprintf(i18n.T("html.image"), alt))
	case "td", "th":
		if r.cellSep {
			r.rawText(" | ")
		}
		r.cellSep = true
	case "tr":
		r.newline()
		r.cellSep = false
	default:
		if blockTags[name] {
			r.newline()
		}
	}
}

func (r *htmlRenderer) close(name string) {
	if skipTags[name] {
		if r.skipDepth > 0 {
			r.skipDepth--
		}
		return
	}
	if r.skipDepth > 0 {
		return
	}
	switch name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		r.newline()
		r.heading = 0
	case "b", "strong":
		r.flushInline()
		r.bold = max(r.bold-1, 0)
	case "i", "em":
		r.flushInline()
		r.italic = max(r.italic-1, 0)
	case "code", "tt", "kbd":
		r.flushInline()
		r.mono = max(r.mono-1, 0)
	case "pre":
		r.newline()
		r.pre = false
		r.mono = max(r.mono-1, 0)
	case "blockquote":
		r.newline()
		r.quote = max(r.quote-1, 0)
	case "ul", "ol":
		r.newline()
		r.listDepth = max(r.listDepth-1, 0)
		if len(r.olIndex) > 0 {
			r.olIndex = r.olIndex[:len(r.olIndex)-1]
		}
	case "li":
		r.newline()
	case "a":
		if u := safeURL(r.href); u != "" {
			r.rawText(" (" + u + ")")
		}
		r.href = ""
	case "tr":
		r.newline()
		r.cellSep = false
	default:
		if blockTags[name] {
			r.newline()
		}
	}
}

// safeURL liefert die URL nur für http/https zurück – alles andere
// (javascript:, data:, file:, …) wird verworfen.
func safeURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.String()
	}
	return ""
}

// text fügt Fließtext hinzu (Whitespace kollabiert, außer in <pre>).
func (r *htmlRenderer) text(s string) {
	if r.pre {
		for i, part := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
			if i > 0 {
				r.newline()
			}
			r.rawText(part)
		}
		return
	}
	collapsed := strings.Join(strings.Fields(s), " ")
	if collapsed == "" {
		return
	}
	if !r.lineAtStart() && !strings.HasSuffix(r.line.String(), " ") {
		// Wortgrenze zwischen Textknoten erhalten.
		if strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\n") || strings.HasPrefix(s, "\t") {
			r.line.WriteString(" ")
		}
	}
	r.rawText(collapsed)
	if strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\n") {
		r.line.WriteString(" ")
	}
}

func (r *htmlRenderer) rawText(s string) {
	if s == "" {
		return
	}
	if !r.lineStarted && r.quote > 0 {
		r.line.WriteString(strings.Repeat("> ", r.quote))
	}
	r.line.WriteString(s)
	r.lineStarted = true
	r.lineEmpty = false
}

func (r *htmlRenderer) lineAtStart() bool { return !r.lineStarted && r.line.Len() == 0 }

// style liefert den aktuellen Stil (ohne Inline-Flag).
func (r *htmlRenderer) style() widget.RichTextStyle {
	style := widget.RichTextStyleParagraph
	switch {
	case r.heading >= 1 && r.heading <= 2:
		style = widget.RichTextStyleHeading
	case r.heading >= 3:
		style = widget.RichTextStyleSubHeading
	case r.mono > 0:
		style = widget.RichTextStyleCodeBlock
	}
	style.TextStyle.Bold = style.TextStyle.Bold || r.bold > 0
	style.TextStyle.Italic = r.italic > 0
	if r.mono > 0 {
		style.TextStyle.Monospace = true
	}
	return style
}

// flushInline schließt den aktuellen Text-Lauf als Inline-Segment ab
// (Zeile läuft weiter) – für Stilwechsel mitten in der Zeile.
func (r *htmlRenderer) flushInline() {
	text := r.line.String()
	r.line.Reset()
	if text == "" {
		return
	}
	style := r.style()
	style.Inline = true
	r.segments = append(r.segments, &widget.TextSegment{Text: text, Style: style})
}

// newline schließt die aktuelle Zeile als Segment ab.
func (r *htmlRenderer) newline() {
	r.flush()
}

// flush erzeugt aus der gesammelten Zeile das abschließende (nicht-inline)
// Segment der Zeile.
func (r *htmlRenderer) flush() {
	text := strings.TrimRight(r.line.String(), " ")
	r.line.Reset()
	if text == "" && !r.lineStarted {
		// Leere Zeilen nur einmal hintereinander (Absatzabstand).
		if r.lineEmpty {
			return
		}
		r.lineEmpty = true
		r.segments = append(r.segments, &widget.TextSegment{Text: "", Style: widget.RichTextStyleParagraph})
		return
	}
	r.lineEmpty = false
	r.lineStarted = false
	r.segments = append(r.segments, &widget.TextSegment{Text: text, Style: r.style()})
	if r.truncated {
		r.segments = append(r.segments, &widget.TextSegment{
			Text: "…", Style: widget.RichTextStyleParagraph,
		})
	}
}
