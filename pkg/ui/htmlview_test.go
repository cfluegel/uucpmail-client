package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/widget"
)

// renderedText fügt alle Segmenttexte zusammen (Zeilen durch \n getrennt).
func renderedText(src string) string {
	var sb strings.Builder
	for _, seg := range RenderHTML(src) {
		if ts, ok := seg.(*widget.TextSegment); ok {
			sb.WriteString(ts.Text)
			if !ts.Style.Inline {
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func TestHTMLScriptDropped(t *testing.T) {
	out := renderedText(`<p>sichtbar</p><script>alert(1)</script><style>.x{color:red}</style>`)
	if !strings.Contains(out, "sichtbar") {
		t.Errorf("inhalt fehlt: %q", out)
	}
	if strings.Contains(out, "alert") || strings.Contains(out, "color") {
		t.Errorf("script/style nicht entfernt: %q", out)
	}
}

func TestHTMLEventHandlersIgnored(t *testing.T) {
	out := renderedText(`<p onclick="evil()" onmouseover="evil()">text</p>`)
	if strings.Contains(out, "evil") {
		t.Errorf("event-handler sichtbar: %q", out)
	}
	if !strings.Contains(out, "text") {
		t.Errorf("text fehlt: %q", out)
	}
}

func TestHTMLDangerousURLsDropped(t *testing.T) {
	out := renderedText(`<a href="javascript:alert(1)">klick</a> <a href="data:text/html,x">daten</a> <a href="https://example.com/x">ok</a>`)
	if strings.Contains(out, "javascript") || strings.Contains(out, "data:") {
		t.Errorf("gefährliche URL sichtbar: %q", out)
	}
	if !strings.Contains(out, "klick") || !strings.Contains(out, "daten") {
		t.Errorf("linktexte fehlen: %q", out)
	}
	if !strings.Contains(out, "(https://example.com/x)") {
		t.Errorf("http-URL fehlt: %q", out)
	}
}

func TestHTMLImgBecomesAltText(t *testing.T) {
	out := renderedText(`<img src="http://evil/track.png" onerror="evil()" alt="Logo">`)
	if strings.Contains(out, "evil") || strings.Contains(out, "track.png") {
		t.Errorf("img-attribute sichtbar: %q", out)
	}
	if !strings.Contains(out, "Logo") {
		t.Errorf("alt-text fehlt: %q", out)
	}
}

func TestHTMLStructure(t *testing.T) {
	src := `<h1>Titel</h1><p>Absatz mit <b>fett</b> und <i>kursiv</i>.</p>
<ul><li>eins</li><li>zwei</li></ul>
<ol><li>erstens</li></ol>
<blockquote>zitat</blockquote>
<pre>  code  bleibt</pre>
<table><tr><td>a</td><td>b</td></tr></table>`
	out := renderedText(src)
	for _, want := range []string{"Titel", "fett", "kursiv", "• eins", "• zwei", "1. erstens", "> zitat", "  code  bleibt", "a | b"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q fehlt in:\n%s", want, out)
		}
	}
	// Heading-Stil prüfen.
	segs := RenderHTML(src)
	foundHeading := false
	for _, seg := range segs {
		if ts, ok := seg.(*widget.TextSegment); ok && ts.Text == "Titel" {
			if ts.Style.SizeName == widget.RichTextStyleHeading.SizeName {
				foundHeading = true
			}
		}
	}
	if !foundHeading {
		t.Error("h1 nicht als Heading gerendert")
	}
}

func TestHTMLBrokenMarkupNoPanic(t *testing.T) {
	cases := []string{
		`<b><i>nie geschlossen`,
		`</p></div></b>nur schließend`,
		`<<<>>><p<><script`,
		strings.Repeat("<div>", 500) + "tief" + strings.Repeat("</div>", 10),
		`<a href=">unquoted`,
	}
	for _, c := range cases {
		_ = renderedText(c) // darf nicht panicen
	}
}

func TestHTMLEntitiesDecoded(t *testing.T) {
	out := renderedText(`<p>K&ouml;ln &amp; Umgebung &lt;test&gt;</p>`)
	if !strings.Contains(out, "Köln & Umgebung <test>") {
		t.Errorf("entities: %q", out)
	}
}

func TestHTMLToText(t *testing.T) {
	got := htmlToText(`<p>Zeile1</p><p>Zeile2 <b>fett</b></p>`)
	if !strings.Contains(got, "Zeile1") || !strings.Contains(got, "Zeile2 fett") {
		t.Errorf("htmlToText: %q", got)
	}
}
