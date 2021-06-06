package main

import (
	"io"
	"net/textproto"
	"testing"

	"github.com/jordan-wright/email"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNewAttachmentReader(t *testing.T) {
	base64Headers := textproto.MIMEHeader{}
	base64Headers.Add("Content-Transfer-Encoding", "base64")

	noHeaders := textproto.MIMEHeader{}

	tests := []struct {
		headers  *textproto.MIMEHeader
		content  []byte
		expected []byte
	}{
		{&base64Headers, []byte("dGVzdA=="), []byte("test")},
		{&noHeaders, []byte("test"), []byte("test")},
		{&base64Headers, []byte("test%"), []byte("test%")},
	}

	for _, test := range tests {
		attachment := &email.Attachment{Filename: "test", Content: test.content, Header: *test.headers}

		r := NewAttachmentReader(attachment)
		data, err := io.ReadAll(r)

		require.Nil(t, err, "should be able to read attachment")
		assert.Equal(t, test.expected, data)
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
