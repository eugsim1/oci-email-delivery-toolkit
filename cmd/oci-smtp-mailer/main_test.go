package main

import (
	"net/mail"
	"strings"
	"testing"
)

func TestParseAddressList(t *testing.T) {
	addresses, err := parseAddressList("OCI_EMAIL_TO", "Alice <alice@example.com>, bob@example.net", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(addresses) != 2 || addresses[1].Address != "bob@example.net" {
		t.Fatalf("unexpected addresses: %#v", addresses)
	}
}

func TestHeaderInjectionRejected(t *testing.T) {
	if err := rejectHeaderInjection("hello\r\nBcc: victim@example.com"); err == nil {
		t.Fatal("expected header injection to be rejected")
	}
}

func TestBuildMessageIncludesAlternativeBodies(t *testing.T) {
	cfg := config{
		from:     mail.Address{Name: "Example", Address: "no-reply@example.com"},
		to:       []mail.Address{{Address: "user@example.net"}},
		subject:  "Hello",
		textBody: "Plain text",
		htmlBody: "<p>HTML body</p>",
	}
	message, err := buildMessage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"multipart/alternative", "Plain text", "<p>HTML body</p>", "Message-ID:"} {
		if !strings.Contains(string(message), wanted) {
			t.Fatalf("message does not contain %q", wanted)
		}
	}
}
