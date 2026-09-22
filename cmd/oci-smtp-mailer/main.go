package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultMaxMessageBytes int64 = 2_000_000

type attachment struct {
	name        string
	contentType string
	data        []byte
}

type attachmentFlags []string

func (values *attachmentFlags) String() string {
	return strings.Join(*values, string(os.PathListSeparator))
}

func (values *attachmentFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("attachment path must not be empty")
	}
	*values = append(*values, value)
	return nil
}

type config struct {
	host            string
	port            int
	mode            string
	username        string
	password        string
	from            mail.Address
	to              []mail.Address
	cc              []mail.Address
	subject         string
	textBody        string
	htmlBody        string
	timeout         time.Duration
	attachments     []attachment
	maxMessageBytes int64
}

func main() {
	dryRun := flag.Bool("dry-run", false, "write the MIME message to stdout without connecting to OCI")
	maxMessageBytes := flag.Int64("max-message-bytes", 0, "override OCI_EMAIL_MAX_BYTES for the encoded MIME message")
	var attachmentPaths attachmentFlags
	flag.Var(&attachmentPaths, "attachment", "path to a regular file to attach; repeat for multiple files")
	flag.Parse()

	cfg, err := loadConfig(*dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(2)
	}
	if *maxMessageBytes < 0 {
		fmt.Fprintln(os.Stderr, "configuration error: -max-message-bytes must be greater than zero")
		os.Exit(2)
	}
	if *maxMessageBytes > 0 {
		cfg.maxMessageBytes = *maxMessageBytes
	}

	paths := append(splitAttachmentPaths(os.Getenv("OCI_EMAIL_ATTACHMENTS")), attachmentPaths...)
	cfg.attachments, err = loadAttachments(paths, cfg.maxMessageBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "attachment error:", err)
		os.Exit(2)
	}

	message, err := buildMessage(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "message error:", err)
		os.Exit(2)
	}

	if *dryRun {
		_, _ = os.Stdout.Write(message)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	if err := send(ctx, cfg, message); err != nil {
		fmt.Fprintln(os.Stderr, "send failed:", err)
		os.Exit(1)
	}
	fmt.Printf("Email accepted by OCI Email Delivery for %d recipient(s).\n", len(cfg.to)+len(cfg.cc))
}

func loadConfig(dryRun bool) (config, error) {
	var cfg config
	var err error

	cfg.host = strings.TrimSpace(os.Getenv("OCI_SMTP_HOST"))
	cfg.mode = strings.ToLower(strings.TrimSpace(envOr("OCI_SMTP_MODE", "tls")))
	cfg.username = os.Getenv("OCI_SMTP_USERNAME")
	cfg.password = os.Getenv("OCI_SMTP_PASSWORD")
	cfg.subject = envOr("OCI_EMAIL_SUBJECT", "OCI Email Delivery test")
	cfg.textBody = os.Getenv("OCI_EMAIL_TEXT")
	cfg.htmlBody = os.Getenv("OCI_EMAIL_HTML")
	cfg.maxMessageBytes, err = strconv.ParseInt(envOr("OCI_EMAIL_MAX_BYTES", strconv.FormatInt(defaultMaxMessageBytes, 10)), 10, 64)
	if err != nil || cfg.maxMessageBytes <= 0 {
		return cfg, errors.New("OCI_EMAIL_MAX_BYTES must be a positive integer number of bytes")
	}

	cfg.port, err = strconv.Atoi(envOr("OCI_SMTP_PORT", "465"))
	if err != nil || cfg.port < 1 || cfg.port > 65535 {
		return cfg, errors.New("OCI_SMTP_PORT must be a number between 1 and 65535")
	}
	cfg.timeout, err = time.ParseDuration(envOr("OCI_SMTP_TIMEOUT", "30s"))
	if err != nil || cfg.timeout <= 0 {
		return cfg, errors.New("OCI_SMTP_TIMEOUT must be a positive Go duration such as 30s")
	}
	if cfg.mode != "tls" && cfg.mode != "starttls" {
		return cfg, errors.New("OCI_SMTP_MODE must be tls or starttls")
	}
	if !dryRun && cfg.host == "" {
		return cfg, errors.New("OCI_SMTP_HOST is required")
	}
	if !dryRun && (cfg.username == "" || cfg.password == "") {
		return cfg, errors.New("OCI_SMTP_USERNAME and OCI_SMTP_PASSWORD are required")
	}

	cfg.from, err = parseOneAddress("OCI_EMAIL_FROM", os.Getenv("OCI_EMAIL_FROM"))
	if err != nil {
		return cfg, err
	}
	cfg.to, err = parseAddressList("OCI_EMAIL_TO", os.Getenv("OCI_EMAIL_TO"), true)
	if err != nil {
		return cfg, err
	}
	cfg.cc, err = parseAddressList("OCI_EMAIL_CC", os.Getenv("OCI_EMAIL_CC"), false)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.textBody) == "" && strings.TrimSpace(cfg.htmlBody) == "" {
		return cfg, errors.New("set OCI_EMAIL_TEXT, OCI_EMAIL_HTML, or both")
	}
	if err := rejectHeaderInjection(cfg.subject); err != nil {
		return cfg, fmt.Errorf("OCI_EMAIL_SUBJECT: %w", err)
	}
	return cfg, nil
}

func send(ctx context.Context, cfg config, message []byte) error {
	address := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	dialer := &net.Dialer{Timeout: cfg.timeout}
	tlsConfig := &tls.Config{ServerName: cfg.host, MinVersion: tls.VersionTLS12}

	var client *smtp.Client
	var baseConn net.Conn
	if cfg.mode == "tls" {
		conn, err := tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
		if err != nil {
			return fmt.Errorf("TLS connection to %s: %w", address, err)
		}
		client, err = smtp.NewClient(conn, cfg.host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("SMTP client: %w", err)
		}
		baseConn = conn
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf("connection to %s: %w", address, err)
		}
		client, err = smtp.NewClient(conn, cfg.host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("SMTP client: %w", err)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return fmt.Errorf("STARTTLS: %w", err)
		}
		baseConn = conn
	}
	defer client.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = baseConn.SetDeadline(deadline)
	}

	auth := smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication: %w", err)
	}
	if err := client.Mail(cfg.from.Address); err != nil {
		return fmt.Errorf("MAIL FROM rejected: %w", err)
	}
	for _, recipient := range append(append([]mail.Address{}, cfg.to...), cfg.cc...) {
		if err := client.Rcpt(recipient.Address); err != nil {
			return fmt.Errorf("RCPT TO %s rejected: %w", recipient.Address, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA command: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("message upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("message not accepted: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("SMTP quit: %w", err)
	}
	return nil
}

func buildMessage(cfg config) ([]byte, error) {
	var out bytes.Buffer
	writeHeader := func(name, value string) {
		fmt.Fprintf(&out, "%s: %s\r\n", name, value)
	}

	writeHeader("From", cfg.from.String())
	writeHeader("To", formatAddresses(cfg.to))
	if len(cfg.cc) > 0 {
		writeHeader("Cc", formatAddresses(cfg.cc))
	}
	writeHeader("Subject", mime.QEncoding.Encode("UTF-8", cfg.subject))
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	writeHeader("Message-ID", newMessageID(domainOf(cfg.from.Address)))
	writeHeader("MIME-Version", "1.0")

	bodyHeader, body, err := buildBodyEntity(cfg)
	if err != nil {
		return nil, err
	}

	if len(cfg.attachments) == 0 {
		writeMIMEHeaders(&out, bodyHeader)
		out.WriteString("\r\n")
		out.Write(body)
		return enforceMessageSize(out.Bytes(), cfg.maxMessageBytes)
	}

	mixed := multipart.NewWriter(&out)
	writeHeader("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%q", mixed.Boundary()))
	out.WriteString("\r\n")

	bodyPart, err := mixed.CreatePart(bodyHeader)
	if err != nil {
		return nil, fmt.Errorf("create message body part: %w", err)
	}
	if _, err := bodyPart.Write(body); err != nil {
		return nil, fmt.Errorf("write message body part: %w", err)
	}

	for _, file := range cfg.attachments {
		if err := writeAttachment(mixed, file); err != nil {
			return nil, err
		}
	}
	if err := mixed.Close(); err != nil {
		return nil, fmt.Errorf("close mixed MIME body: %w", err)
	}
	return enforceMessageSize(out.Bytes(), cfg.maxMessageBytes)
}

func buildBodyEntity(cfg config) (textproto.MIMEHeader, []byte, error) {
	header := make(textproto.MIMEHeader)
	if cfg.htmlBody == "" {
		header.Set("Content-Type", "text/plain; charset=UTF-8")
		header.Set("Content-Transfer-Encoding", "8bit")
		return header, []byte(normalizeCRLF(cfg.textBody)), nil
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	header.Set("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", mw.Boundary()))
	textBody := cfg.textBody
	if strings.TrimSpace(textBody) == "" {
		textBody = "This message contains an HTML version."
	}
	if err := writePart(mw, "text/plain; charset=UTF-8", textBody); err != nil {
		return nil, nil, err
	}
	if err := writePart(mw, "text/html; charset=UTF-8", cfg.htmlBody); err != nil {
		return nil, nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, nil, fmt.Errorf("close alternative MIME body: %w", err)
	}
	return header, body.Bytes(), nil
}

func writePart(mw *multipart.Writer, contentType, body string) error {
	header := make(textproto.MIMEHeader)
	header["Content-Type"] = []string{contentType}
	header["Content-Transfer-Encoding"] = []string{"8bit"}
	part, err := mw.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create MIME part: %w", err)
	}
	if _, err := io.WriteString(part, normalizeCRLF(body)); err != nil {
		return fmt.Errorf("write MIME part: %w", err)
	}
	return nil
}

func writeAttachment(mw *multipart.Writer, file attachment) error {
	header := make(textproto.MIMEHeader)
	contentType := mime.FormatMediaType(file.contentType, map[string]string{"name": file.name})
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.name})
	if contentType == "" || disposition == "" {
		return fmt.Errorf("attachment %q has a filename or media type that cannot be encoded safely", file.name)
	}
	header.Set("Content-Type", contentType)
	header.Set("Content-Disposition", disposition)
	header.Set("Content-Transfer-Encoding", "base64")
	part, err := mw.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create attachment %q: %w", file.name, err)
	}
	if err := writeBase64(part, file.data); err != nil {
		return fmt.Errorf("encode attachment %q: %w", file.name, err)
	}
	return nil
}

func writeBase64(w io.Writer, data []byte) error {
	wrapped := &mimeLineWriter{writer: w}
	encoder := base64.NewEncoder(base64.StdEncoding, wrapped)
	if _, err := encoder.Write(data); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return wrapped.finish()
}

type mimeLineWriter struct {
	writer io.Writer
	column int
}

func (wrapped *mimeLineWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		remaining := 76 - wrapped.column
		if len(data) < remaining {
			remaining = len(data)
		}
		n, err := wrapped.writer.Write(data[:remaining])
		written += n
		wrapped.column += n
		if err != nil {
			return written, err
		}
		if n != remaining {
			return written, io.ErrShortWrite
		}
		data = data[remaining:]
		if wrapped.column == 76 {
			if _, err := io.WriteString(wrapped.writer, "\r\n"); err != nil {
				return written, err
			}
			wrapped.column = 0
		}
	}
	return written, nil
}

func (wrapped *mimeLineWriter) finish() error {
	if wrapped.column == 0 {
		return nil
	}
	_, err := io.WriteString(wrapped.writer, "\r\n")
	wrapped.column = 0
	return err
}

func writeMIMEHeaders(w io.Writer, header textproto.MIMEHeader) {
	for _, name := range []string{"Content-Type", "Content-Transfer-Encoding"} {
		for _, value := range header.Values(name) {
			fmt.Fprintf(w, "%s: %s\r\n", name, value)
		}
	}
}

func enforceMessageSize(message []byte, limit int64) ([]byte, error) {
	if limit > 0 && int64(len(message)) > limit {
		return nil, fmt.Errorf("encoded MIME message is %d bytes, exceeding the configured %d-byte limit; reduce the body or attachments, or use an OCI-approved higher limit", len(message), limit)
	}
	return message, nil
}

func splitAttachmentPaths(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	paths := filepath.SplitList(value)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			result = append(result, path)
		}
	}
	return result
}

func loadAttachments(paths []string, maxMessageBytes int64) ([]attachment, error) {
	result := make([]attachment, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, errors.New("attachment path must not be empty")
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", path, err)
		}
		absolutePath = filepath.Clean(absolutePath)
		if _, exists := seen[absolutePath]; exists {
			return nil, fmt.Errorf("attachment %q was specified more than once", path)
		}
		seen[absolutePath] = struct{}{}

		opened, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %q: %w", path, err)
		}
		info, statErr := opened.Stat()
		if statErr != nil {
			_ = opened.Close()
			return nil, fmt.Errorf("inspect %q: %w", path, statErr)
		}
		if !info.Mode().IsRegular() {
			_ = opened.Close()
			return nil, fmt.Errorf("%q is not a regular file", path)
		}
		if maxMessageBytes > 0 && info.Size() > maxMessageBytes {
			_ = opened.Close()
			return nil, fmt.Errorf("attachment %q is %d bytes before MIME encoding, exceeding the configured %d-byte message limit", path, info.Size(), maxMessageBytes)
		}
		var reader io.Reader = opened
		if maxMessageBytes > 0 {
			reader = io.LimitReader(opened, maxMessageBytes+1)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := opened.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %q: %w", path, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %q: %w", path, closeErr)
		}
		if maxMessageBytes > 0 && int64(len(data)) > maxMessageBytes {
			return nil, fmt.Errorf("attachment %q grew beyond the configured %d-byte message limit while it was read", path, maxMessageBytes)
		}

		name := filepath.Base(path)
		if err := rejectHeaderInjection(name); err != nil {
			return nil, fmt.Errorf("attachment filename %q: %w", name, err)
		}
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if contentType == "" {
			contentType = "application/octet-stream"
		} else {
			mediaType, _, parseErr := mime.ParseMediaType(contentType)
			if parseErr != nil {
				contentType = "application/octet-stream"
			} else {
				contentType = mediaType
			}
		}
		result = append(result, attachment{name: name, contentType: contentType, data: data})
	}
	return result, nil
}

func parseOneAddress(name, value string) (mail.Address, error) {
	if strings.TrimSpace(value) == "" {
		return mail.Address{}, fmt.Errorf("%s is required", name)
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil {
		return mail.Address{}, fmt.Errorf("%s: %w", name, err)
	}
	return *parsed, nil
}

func parseAddressList(name, value string, required bool) ([]mail.Address, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return nil, fmt.Errorf("%s is required", name)
		}
		return nil, nil
	}
	parsed, err := mail.ParseAddressList(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	result := make([]mail.Address, 0, len(parsed))
	for _, address := range parsed {
		result = append(result, *address)
	}
	return result, nil
}

func formatAddresses(addresses []mail.Address) string {
	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address.String())
	}
	return strings.Join(values, ", ")
}

func rejectHeaderInjection(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("must not contain CR or LF characters")
	}
	return nil
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func newMessageID(domain string) string {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), domain)
	}
	return fmt.Sprintf("<%s@%s>", hex.EncodeToString(random), domain)
}

func domainOf(address string) string {
	parts := strings.Split(address, "@")
	if len(parts) == 2 && parts[1] != "" {
		return parts[1]
	}
	return "localhost"
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
