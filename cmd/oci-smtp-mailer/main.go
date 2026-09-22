package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
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
	"strconv"
	"strings"
	"time"
)

type config struct {
	host     string
	port     int
	mode     string
	username string
	password string
	from     mail.Address
	to       []mail.Address
	cc       []mail.Address
	subject  string
	textBody string
	htmlBody string
	timeout  time.Duration
}

func main() {
	dryRun := flag.Bool("dry-run", false, "write the MIME message to stdout without connecting to OCI")
	flag.Parse()

	cfg, err := loadConfig(*dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
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

	if cfg.htmlBody == "" {
		writeHeader("Content-Type", "text/plain; charset=UTF-8")
		writeHeader("Content-Transfer-Encoding", "8bit")
		out.WriteString("\r\n")
		out.WriteString(normalizeCRLF(cfg.textBody))
		return out.Bytes(), nil
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	writeHeader("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", mw.Boundary()))
	out.WriteString("\r\n")
	textBody := cfg.textBody
	if strings.TrimSpace(textBody) == "" {
		textBody = "This message contains an HTML version."
	}
	if err := writePart(mw, "text/plain; charset=UTF-8", textBody); err != nil {
		return nil, err
	}
	if err := writePart(mw, "text/html; charset=UTF-8", cfg.htmlBody); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close MIME body: %w", err)
	}
	out.Write(body.Bytes())
	return out.Bytes(), nil
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
