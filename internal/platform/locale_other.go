//go:build !windows && !darwin

package platform

// osLocale reports the OS locale when the platform does not expose it through
// the POSIX environment variables. Mobile hosts use SetLocale instead.
func osLocale() string { return "" }
