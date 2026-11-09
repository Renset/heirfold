// Package mail sends the letters of Heirfold: sign-in codes and invitations.
//
// A letter is plain text. The package has three transports: an SMTP server,
// the HTTP API of Resend, and the server log for a computer without mail.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is one letter.
type Message struct {
	To      string
	Subject string
	Text    string
	// Kind names the letter in the log, for example "code" or "invite".
	Kind string
	// UserID is the account that the letter is for.
	UserID int64
	// Code is the sign-in code of the letter, if it has one. Only the log
	// transport reads it.
	Code string
}

// Sender is a transport for letters.
type Sender interface {
	// Send delivers one letter.
	Send(ctx context.Context, m Message) error
	// Name returns the name of the transport: smtp, resend or log.
	Name() string
}

// Log writes the sign-in code to the server log in place of a letter. It is
// for a server on the computer of its administrator. The log line holds the
// account number and not the address.
type Log struct {
	Logger *slog.Logger
}

// Name returns "log".
func (Log) Name() string { return "log" }

// Send writes one line to the log.
func (l Log) Send(_ context.Context, m Message) error {
	if m.Code != "" {
		l.Logger.Info("letter not sent, the sign-in code is here", "kind", m.Kind, "user", m.UserID, "code", m.Code)
		return nil
	}
	l.Logger.Info("letter not sent", "kind", m.Kind, "user", m.UserID)
	return nil
}

// ParseFrom checks the sender of the letters. It accepts "Name <a@b.c>" and
// "a@b.c".
func ParseFrom(text string) (*netmail.Address, error) {
	if strings.ContainsAny(text, "\r\n") {
		return nil, errors.New("mail: the sender has a line break")
	}
	address, err := netmail.ParseAddress(text)
	if err != nil {
		return nil, fmt.Errorf("mail: bad sender address: %w", err)
	}
	return address, nil
}

func checkMessage(m Message) error {
	if strings.ContainsAny(m.To, "\r\n") || strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("mail: a header has a line break")
	}
	if parsed, err := netmail.ParseAddress(m.To); err != nil || parsed.Address != m.To {
		return errors.New("mail: bad recipient address")
	}
	return nil
}

// SMTP sends letters through an SMTP server.
type SMTP struct {
	Host     string
	Port     string
	User     string
	Password string
	From     *netmail.Address
	// ImplicitTLS opens the connection with TLS, as port 465 needs. Without
	// it the client upgrades the connection with STARTTLS.
	ImplicitTLS bool
	// Timeout limits one delivery.
	Timeout time.Duration
}

// Name returns "smtp".
func (*SMTP) Name() string { return "smtp" }

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Send delivers one letter. It refuses a connection without TLS, except to a
// server on the same computer.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if err := checkMessage(m); err != nil {
		return err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	address := net.JoinHostPort(s.Host, s.Port)
	config := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if s.ImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: config}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("mail: connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("mail: greeting: %w", err)
	}
	defer func() { _ = client.Close() }()
	if !s.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(config); err != nil {
				return fmt.Errorf("mail: STARTTLS: %w", err)
			}
		} else if !isLocalHost(s.Host) {
			return errors.New("mail: the server offers no STARTTLS, so the letter would travel in the clear")
		}
	}
	if s.User != "" {
		// PlainAuth itself refuses to send the password without TLS.
		if err := client.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: sign-in to the server failed: %w", err)
		}
	}
	if err := client.Mail(s.From.Address); err != nil {
		return fmt.Errorf("mail: sender refused: %w", err)
	}
	if err := client.Rcpt(m.To); err != nil {
		return fmt.Errorf("mail: recipient refused: %w", err)
	}
	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: data: %w", err)
	}
	if _, err := body.Write(Build(s.From, m, time.Now())); err != nil {
		return fmt.Errorf("mail: data: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("mail: data: %w", err)
	}
	return client.Quit()
}

// maxLine is the line length that RFC 5322 recommends.
const maxLine = 78

// fold writes one header field. A long value breaks at its spaces, so each
// line stays within maxLine where the words allow it. An encoded word is at
// most 75 characters, so it always fits on a line of its own.
func fold(name, value string) string {
	var b strings.Builder
	line := name + ":"
	for _, word := range strings.Split(value, " ") {
		if len(line)+1+len(word) > maxLine {
			b.WriteString(line)
			b.WriteString("\r\n")
			line = ""
		}
		line += " " + word
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}

// encodeSubject returns the subject for the header. Plain ASCII stays as it
// is. Other text becomes encoded words. The first word is shorter, so the
// first line with "Subject: " also fits into maxLine.
func encodeSubject(subject string) string {
	plain := true
	for i := 0; i < len(subject); i++ {
		if subject[i] < 0x20 || subject[i] > 0x7e {
			plain = false
			break
		}
	}
	if plain && !strings.Contains(subject, "=?") {
		return subject
	}
	const head, tail = "=?utf-8?q?", "?="
	var words []string
	var word strings.Builder
	limit := maxLine - len("Subject: ") - len(head) - len(tail)
	for _, r := range subject {
		part := qEncode(string(r))
		if word.Len()+len(part) > limit {
			words = append(words, head+word.String()+tail)
			word.Reset()
			limit = 75 - len(head) - len(tail)
		}
		word.WriteString(part)
	}
	if word.Len() > 0 {
		words = append(words, head+word.String()+tail)
	}
	return strings.Join(words, " ")
}

// qEncode writes text in the Q encoding of RFC 2047. Spaces become "_".
func qEncode(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == ' ':
			b.WriteByte('_')
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("!*+-/", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	return b.String()
}

// Build returns the letter in the Internet Message Format. The body is
// base64, so every line is short and holds only ASCII.
func Build(from *netmail.Address, m Message, now time.Time) []byte {
	var id [12]byte
	_, _ = rand.Read(id[:])
	domain := "localhost"
	if at := strings.LastIndexByte(from.Address, '@'); at >= 0 {
		domain = from.Address[at+1:]
	}
	var b bytes.Buffer
	header := func(name, value string) { b.WriteString(fold(name, value)) }
	header("From", from.String())
	header("To", (&netmail.Address{Address: m.To}).String())
	header("Subject", encodeSubject(m.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id[:])+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "base64")
	header("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(m.Text))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteString("\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded)
	b.WriteString("\r\n")
	return b.Bytes()
}

// ResendEndpoint is the address of the Resend API.
const ResendEndpoint = "https://api.resend.com/emails"

// Resend sends letters through the HTTP API of Resend. It is for a host that
// blocks the SMTP ports.
type Resend struct {
	Key  string
	From *netmail.Address
	// Endpoint replaces ResendEndpoint in tests.
	Endpoint string
	// Client replaces the default HTTP client in tests.
	Client *http.Client
}

// Name returns "resend".
func (*Resend) Name() string { return "resend" }

var resendClient = &http.Client{
	Timeout: 30 * time.Second,
	// The API never redirects. A redirect must not carry the key elsewhere.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// plainFrom returns the sender as readable text: "Name <a@b.c>". A JSON API
// takes the name as it is. The encoded form of a mail header would reach the
// reader as a row of codes.
func plainFrom(from *netmail.Address) string {
	if from.Name == "" {
		return from.Address
	}
	name := from.Name
	if strings.ContainsAny(name, `()<>[]:;@\,."`) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name + " <" + from.Address + ">"
}

// Send delivers one letter.
func (r *Resend) Send(ctx context.Context, m Message) error {
	if err := checkMessage(m); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"from": plainFrom(r.From), "to": []string{m.To}, "subject": m.Subject, "text": m.Text,
	})
	if err != nil {
		return err
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = ResendEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+r.Key)
	request.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = resendClient
	}
	response, err := client.Do(request) // #nosec G107,G704 -- the address is a constant of this package.
	if err != nil {
		// The error of the client can hold the address but never the key.
		return fmt.Errorf("mail: request to the mail service failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("mail: the mail service answered with status %d", response.StatusCode)
	}
	return nil
}
