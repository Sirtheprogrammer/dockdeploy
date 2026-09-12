package api

import (
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"
)

// fields accumulates per-field validation messages so a form reports every
// problem at once instead of one per round trip.
type fields map[string]string

func (f fields) add(name, message string) {
	if _, exists := f[name]; !exists {
		f[name] = message
	}
}

// err returns a validation error, or nil when everything passed.
func (f fields) err() error {
	if len(f) == 0 {
		return nil
	}
	return Invalid(f)
}

// email validates and normalises an address.
func (f fields) email(name, value string) string {
	normalised := strings.ToLower(strings.TrimSpace(value))
	if normalised == "" {
		f.add(name, "Email is required.")
		return ""
	}
	if len(normalised) > 254 {
		f.add(name, "Email is too long.")
		return normalised
	}
	if _, err := mail.ParseAddress(normalised); err != nil {
		f.add(name, "Enter a valid email address.")
	}
	return normalised
}

// required trims and checks a text field against a length range.
func (f fields) required(name, value string, minLen, maxLen int) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		f.add(name, "This field is required.")
	case utf8.RuneCountInString(trimmed) < minLen:
		f.add(name, "Must be at least "+plural(minLen, "character")+".")
	case utf8.RuneCountInString(trimmed) > maxLen:
		f.add(name, "Must be at most "+plural(maxLen, "character")+".")
	}
	return trimmed
}

func plural(n int, unit string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
