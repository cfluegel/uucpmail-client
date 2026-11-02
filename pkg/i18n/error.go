package i18n

import "fmt"

// Error ist ein lazy übersetzender Fehler: der Text wird erst bei Error()
// in der dann aktiven Sprache gerendert. Dadurch funktionieren Sentinel-
// Fehler als Paket-Variablen (Init-Reihenfolge egal, Identität bleibt für
// errors.Is erhalten), die GUI zeigt die konfigurierte Sprache und die
// CLI (ohne Init, Default en) automatisch Englisch. Key+Args lassen sich
// zudem persistieren und später in beliebiger Sprache neu rendern.
type Error struct {
	Key     string
	Args    []any
	Wrapped error
}

func (e *Error) Error() string {
	msg := T(e.Key)
	if len(e.Args) > 0 {
		msg = fmt.Sprintf(msg, e.Args...)
	}
	if e.Wrapped != nil {
		return msg + ": " + e.Wrapped.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Wrapped }

// Errorf erzeugt einen übersetzbaren Fehler; key muss in beiden Tabellen
// stehen, args füllen die %-Platzhalter des übersetzten Formats.
func Errorf(key string, args ...any) error {
	return &Error{Key: key, Args: args}
}

// Wrap hängt einen übersetzbaren Kontext vor err (analog fmt.Errorf mit
// abschließendem ": %w").
func Wrap(err error, key string, args ...any) error {
	return &Error{Key: key, Args: args, Wrapped: err}
}
