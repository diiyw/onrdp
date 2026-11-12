//go:build windows

package platform

import "golang.org/x/sys/windows"

// osLocale reads Windows display-language preferences, which are not exposed
// through the POSIX locale environment variables.
func osLocale() string {
	languages, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err == nil {
		for _, language := range languages {
			if language != "" {
				return language
			}
		}
	}
	return ""
}
