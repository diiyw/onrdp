package i18n

import (
	"errors"
	"testing"
)

func TestEveryCatalogCoversEveryKey(t *testing.T) {
	for _, language := range Languages() {
		catalog, ok := catalogs[language.Code]
		if !ok {
			t.Fatalf("missing catalog %q", language.Code)
		}
		if len(catalog) == 0 {
			t.Fatalf("catalog %q is empty", language.Code)
		}
		for key := range catalogs["en"] {
			if _, ok := catalog[key]; !ok {
				t.Fatalf("catalog %q is missing key %q", language.Code, key)
			}
		}
		for key := range catalog {
			if _, ok := catalogs["en"][key]; !ok {
				t.Fatalf("catalog %q has a key %q that English lacks", language.Code, key)
			}
		}
	}
}

func TestMatchResolvesLocales(t *testing.T) {
	for _, tt := range []struct{ locale, want string }{
		{"zh_CN.UTF-8", "zh-CN"},
		{"zh-Hant-TW", "zh-TW"},
		{"zh-HK", "zh-TW"},
		{"zh", "zh-CN"},
		{"en_US", "en"},
		{"ja_JP.UTF-8", "ja"},
		{"de_DE", "en"},
		{"", "en"},
		{"POSIX", "en"},
	} {
		if got := Match(tt.locale); got != tt.want {
			t.Fatalf("Match(%q) = %q, want %q", tt.locale, got, tt.want)
		}
	}
}

func TestResolvePrefersSupportedPreference(t *testing.T) {
	if got := Resolve("ja", "en_US"); got != "ja" {
		t.Fatalf("Resolve(ja, en_US) = %q", got)
	}
	if got := Resolve("", "zh_TW"); got != "zh-TW" {
		t.Fatalf("Resolve(\"\", zh_TW) = %q", got)
	}
	if got := Resolve("de", "ja_JP"); got != "ja" {
		t.Fatalf("Resolve(de, ja_JP) = %q", got)
	}
}

func TestTextFallsBack(t *testing.T) {
	if got := Text("ja", "设置"); got != "設定" {
		t.Fatalf("Text(ja) = %q", got)
	}
	if got := Text("ja", "no such key"); got != "no such key" {
		t.Fatalf("missing key = %q", got)
	}
	if got := Text("en", "%d 台主机", 3); got != "3 hosts" {
		t.Fatalf("formatted text = %q", got)
	}
}

func TestErrorTextTranslates(t *testing.T) {
	err := NewError("端口必须介于 1 和 65535 之间")
	if got := ErrorText("en", err); got != "The port must be between 1 and 65535" {
		t.Fatalf("ErrorText = %q", got)
	}
	plain := errors.New("raw failure")
	if got := ErrorText("ja", plain); got != "raw failure" {
		t.Fatalf("plain error = %q", got)
	}
	if got := ErrorText("ja", nil); got != "" {
		t.Fatalf("nil error = %q", got)
	}
}
