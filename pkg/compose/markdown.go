package compose

// MarkdownSnippet beschreibt einen Toolbar-Eintrag des Compose-Fensters.
// V1 rendert kein Markdown – die Snippets werden als Text an der
// Cursorposition eingefügt (Prefix vor, Suffix nach der Auswahl).
type MarkdownSnippet struct {
	LabelKey string // i18n-Schlüssel
	Prefix   string
	Suffix   string
	Line     bool // true = wirkt auf Zeilenanfang (Überschriften, Listen, Zitat)
}

var MarkdownSnippets = []MarkdownSnippet{
	{LabelKey: "md.h1", Prefix: "# ", Line: true},
	{LabelKey: "md.h2", Prefix: "## ", Line: true},
	{LabelKey: "md.h3", Prefix: "### ", Line: true},
	{LabelKey: "md.bold", Prefix: "**", Suffix: "**"},
	{LabelKey: "md.italic", Prefix: "*", Suffix: "*"},
	{LabelKey: "md.code", Prefix: "`", Suffix: "`"},
	{LabelKey: "md.list", Prefix: "- ", Line: true},
	{LabelKey: "md.numlist", Prefix: "1. ", Line: true},
	{LabelKey: "md.quote", Prefix: "> ", Line: true},
}

// InsertSnippet fügt ein Snippet in text an Position cursor ein (keine
// Auswahl) und liefert den neuen Text samt neuer Cursorposition.
func InsertSnippet(text string, cursor int, sn MarkdownSnippet) (string, int) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	if sn.Line {
		// An den Anfang der aktuellen Zeile setzen.
		lineStart := cursor
		for lineStart > 0 && text[lineStart-1] != '\n' {
			lineStart--
		}
		out := text[:lineStart] + sn.Prefix + text[lineStart:]
		return out, cursor + len(sn.Prefix)
	}
	out := text[:cursor] + sn.Prefix + sn.Suffix + text[cursor:]
	return out, cursor + len(sn.Prefix)
}
