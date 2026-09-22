package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
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

func TestBuildMessageIncludesAttachment(t *testing.T) {
	wantAttachment := []byte("quarter,total\nQ1,42\n")
	cfg := config{
		from:     mail.Address{Name: "Example", Address: "no-reply@example.com"},
		to:       []mail.Address{{Address: "user@example.net"}},
		subject:  "Report",
		textBody: "The report is attached.",
		attachments: []attachment{{
			name:        "report.csv",
			contentType: "text/csv",
			data:        wantAttachment,
		}},
		maxMessageBytes: defaultMaxMessageBytes,
	}

	message, err := buildMessage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, parameters, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "multipart/mixed" {
		t.Fatalf("content type = %q, want multipart/mixed", mediaType)
	}

	reader := multipart.NewReader(parsed.Body, parameters["boundary"])
	bodyPart, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(bodyPart)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != cfg.textBody {
		t.Fatalf("body = %q, want %q", body, cfg.textBody)
	}

	filePart, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	_, dispositionParameters, err := mime.ParseMediaType(filePart.Header.Get("Content-Disposition"))
	if err != nil {
		t.Fatal(err)
	}
	if dispositionParameters["filename"] != "report.csv" {
		t.Fatalf("filename = %q, want report.csv", dispositionParameters["filename"])
	}
	encoded, err := io.ReadAll(filePart)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(encoded)), ""))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, wantAttachment) {
		t.Fatalf("attachment = %q, want %q", decoded, wantAttachment)
	}
}

func TestLoadAttachmentsUsesBasenameAndMediaType(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.txt")
	if err := os.WriteFile(path, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := loadAttachments([]string{path}, defaultMaxMessageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d attachments, want 1", len(files))
	}
	if files[0].name != "report.txt" {
		t.Fatalf("name = %q, want report.txt", files[0].name)
	}
	if files[0].contentType != "text/plain" {
		t.Fatalf("content type = %q, want text/plain", files[0].contentType)
	}
}

func TestLoadAttachmentsRejectsDirectory(t *testing.T) {
	if _, err := loadAttachments([]string{t.TempDir()}, defaultMaxMessageBytes); err == nil {
		t.Fatal("expected a directory attachment to be rejected")
	}
}

func TestLoadAttachmentsRejectsDuplicatePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.bin")
	if err := os.WriteFile(path, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAttachments([]string{path, path}, defaultMaxMessageBytes); err == nil {
		t.Fatal("expected a duplicate attachment to be rejected")
	}
}

func TestWriteBase64UsesMIMELineLength(t *testing.T) {
	source := bytes.Repeat([]byte{0xab}, 200)
	var encoded bytes.Buffer
	if err := writeBase64(&encoded, source); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(encoded.String()), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 line has %d characters, want at most 76", len(line))
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded.String()), ""))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, source) {
		t.Fatal("decoded base64 content differs from source")
	}
}

func TestBuildMessageEnforcesEncodedSizeLimit(t *testing.T) {
	cfg := config{
		from:            mail.Address{Address: "no-reply@example.com"},
		to:              []mail.Address{{Address: "user@example.net"}},
		subject:         "Oversized",
		textBody:        strings.Repeat("x", 256),
		maxMessageBytes: 128,
	}
	if _, err := buildMessage(cfg); err == nil || !strings.Contains(err.Error(), "exceeding") {
		t.Fatalf("expected an encoded message size error, got %v", err)
	}
}
