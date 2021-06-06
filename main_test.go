package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsQuotedPrintable(t *testing.T) {
	tests := []struct {
		input []byte
		want  bool
	}{
		{[]byte("="), false},
		{[]byte("asdf"), true},
	}

	for _, test := range tests {
		assert.Equal(t, test.want, IsQuotedPrintable(test.input))
	}
}

func TestIsBase64(t *testing.T) {
	tests := []struct {
		input []byte
		want  bool
	}{
		{[]byte("%"), false},
		{[]byte("ABC"), false},
		{[]byte("dGVzdA=="), true},
	}

	for _, test := range tests {
		assert.Equal(t, test.want, IsBase64(test.input))
	}
}

func TestIsAllowedEmail(t *testing.T) {
	tests := []struct {
		allow   AllowList
		from    string
		to      []string
		allowed bool
	}{
		{AllowList{ToAddress: "", AllowedEmails: []string{"test@example.com"}}, "test@example.com", []string{"other@example.com", "input@example.com"}, true},
		{AllowList{ToAddress: "", AllowedEmails: []string{"not-test@example.com"}}, "test@example.com", []string{"other@example.com", "input@example.com"}, false},
		{AllowList{ToAddress: "input@example.com", AllowedEmails: []string{"test@example.com"}}, "test@example.com", []string{"other@example.com", "input@example.com"}, true},
		{AllowList{ToAddress: "not-input@example.com", AllowedEmails: []string{"test@example.com"}}, "test@example.com", []string{"other@example.com", "input@example.com"}, false},
	}

	for _, test := range tests {
		assert.Equal(t, test.allowed, test.allow.IsAllowedEmail(test.from, test.to))
	}
}
