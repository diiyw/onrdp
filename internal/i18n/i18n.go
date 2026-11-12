// Package i18n holds the offline message catalogs. Chinese source messages are
// stable keys, so catalogs stay readable next to the code that uses them.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed *.json
var files embed.FS

// Language is one selectable catalog.
type Language struct{ Code, Name string }

// Languages returns the supported catalogs in menu order.
func Languages() []Language {
	return []Language{{"zh-CN", "简体中文"}, {"zh-TW", "繁體中文"}, {"en", "English"}, {"ja", "日本語"}}
}

var catalogs = func() map[string]map[string]string {
	out := make(map[string]map[string]string, len(Languages()))
	for _, language := range Languages() {
		raw, err := files.ReadFile(language.Code + ".json")
		if err != nil {
			panic(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(raw, &messages); err != nil {
			panic(err)
		}
		out[language.Code] = messages
	}
	return out
}()

// Supported reports whether a catalog exists for code.
func Supported(code string) bool { _, ok := catalogs[code]; return ok }

// Catalog exposes one language's messages for coverage tests. Callers must not
// modify the returned map.
func Catalog(code string) map[string]string { return catalogs[code] }

// Match resolves a BCP 47 or POSIX locale to a supported catalog.
func Match(locale string) string {
	locale = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
	locale, _, _ = strings.Cut(locale, ".")
	locale, _, _ = strings.Cut(locale, "@")
	parts := strings.Split(locale, "-")
	if parts[0] == "zh" {
		for _, p := range parts[1:] {
			switch p {
			case "hant", "tw", "hk", "mo":
				return "zh-TW"
			}
		}
		return "zh-CN"
	}
	if Supported(parts[0]) {
		return parts[0]
	}
	return "en"
}

// Resolve prefers an explicit supported preference over the system locale.
func Resolve(preference, system string) string {
	if Supported(preference) {
		return preference
	}
	return Match(system)
}

// Text looks up key, falling back to English and then to the key itself.
func Text(language, key string, args ...any) string {
	value, ok := catalogs[language][key]
	if !ok {
		value, ok = catalogs["en"][key]
	}
	if !ok {
		value = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(value, args...)
	}
	return value
}

// Error is a translatable error carrying the catalog key and its arguments.
type Error struct {
	Key  string
	Args []any
}

func (e *Error) Error() string { return Text("zh-CN", e.Key, e.Args...) }

// NewError builds a translatable error.
func NewError(key string, args ...any) error { return &Error{Key: key, Args: args} }

// ErrorText renders err in language, unwrapping translatable errors.
func ErrorText(language string, err error) string {
	if err == nil {
		return ""
	}
	if translated, ok := err.(*Error); ok {
		return Text(language, translated.Key, translated.Args...)
	}
	return err.Error()
}
