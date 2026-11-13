package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	testFrom = &netmail.Address{Name: "Семейное древо", Address: "tree@example.org"}
	testTime = time.Date(2026, 3, 9, 8, 7, 6, 0, time.FixedZone("", 3*3600))
)

const longText = "Код для входа в семейное древо:\n\n    1234 5678\n\n" +
	"Код действует 10 минут. Введите его на странице, где вы запросили вход.\n\n" +
	"Если вы не запрашивали код, ничего не делайте. Без кода никто не войдёт.\n\nhttps://tree.example.org\n"

func testMessage() Message {
	return Message{To: "anna@example.org", Subject: "Код для входа в семейное древо", Text: longText, Kind: "code", UserID: 42, Code: "12345678"}
}

// splitLetter checks the line ends and returns the header lines and the body lines.
func splitLetter(t *testing.T, raw []byte) (header, body []string) {
	t.Helper()
	if bytes.Count(raw, []byte("\n")) != bytes.Count(raw, []byte("\r\n")) || bytes.Count(raw, []byte("\r")) != bytes.Count(raw, []byte("\r\n")) {
		t.Fatalf("a line does not end with CR LF: %q", raw)
	}
	if !bytes.HasSuffix(raw, []byte("\r\n")) {
		t.Fatalf("the letter does not end with CR LF: %q", raw)
	}
	for _, c := range raw {
		if c > 126 || (c < 32 && c != '\r' && c != '\n') {
			t.Fatalf("the letter holds the byte %#x", c)
		}
	}
	head, rest, found := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !found {
		t.Fatalf("no empty line after the header: %q", raw)
	}
	return strings.Split(string(head), "\r\n"), strings.Split(strings.TrimSuffix(string(rest), "\r\n"), "\r\n")
}

func TestBuild(t *testing.T) {
	m := testMessage()
	raw := Build(testFrom, m, testTime)
	header, body := splitLetter(t, raw)
	for _, line := range header {
		// RFC 5322 forbids more than 998 characters in a line.
		if len(line) > 998 {
			t.Errorf("header line of %d characters", len(line))
		}
	}
	if len(body) < 3 {
		t.Fatalf("the body must have several lines: %q", body)
	}
	for i, line := range body {
		if len(line) > 76 || line == "" || (i < len(body)-1 && len(line) != 76) {
			t.Errorf("body line %d has %d characters", i, len(line))
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(body, ""))
	if err != nil || string(decoded) != m.Text {
		t.Fatalf("body: %q %v", decoded, err)
	}

	letter, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	rawSubject := letter.Header.Get("Subject")
	if !strings.HasPrefix(rawSubject, "=?utf-8?q?") || !strings.HasSuffix(rawSubject, "?=") {
		t.Fatalf("the subject is not Q-encoded: %q", rawSubject)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(rawSubject)
	if err != nil || subject != m.Subject {
		t.Fatalf("subject: %q %v", subject, err)
	}
	from, err := letter.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Address != "tree@example.org" || from[0].Name != "Семейное древо" {
		t.Fatalf("From: %+v %v", from, err)
	}
	to, err := letter.Header.AddressList("To")
	if err != nil || len(to) != 1 || to[0].Address != "anna@example.org" || to[0].Name != "" {
		t.Fatalf("To: %+v %v", to, err)
	}
	date, err := letter.Header.Date()
	if err != nil || !date.Equal(testTime) {
		t.Fatalf("Date: %v %v", date, err)
	}
	for name, want := range map[string]string{
		"Mime-Version":              "1.0",
		"Content-Type":              "text/plain; charset=utf-8",
		"Content-Transfer-Encoding": "base64",
		"Auto-Submitted":            "auto-generated",
	} {
		if got := letter.Header.Get(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	id := letter.Header.Get("Message-Id")
	if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.org>") || len(id) != len("<@example.org>")+24 {
		t.Fatalf("Message-ID: %q", id)
	}
	if other := Build(testFrom, m, testTime); bytes.Equal(other, raw) {
		t.Fatal("two letters share a Message-ID")
	}
	// The log fields of the message stay out of the letter.
	if bytes.Contains(raw, []byte(m.Code)) {
		t.Fatalf("the letter holds the code field of the log: %s", raw)
	}
}

// longSubjects need more than one encoded word.
var longSubjects = []string{
	"Приглашение в семейное древо",
	"Код для входа в семейное древо нашей большой и дружной семьи из города на реке",
	strings.Repeat("Приглашение в семейное древо. ", 12),
	strings.Repeat("я", 200),
	"Invitation to the family tree: " + strings.Repeat("древо ", 30),
}

// subjectLines returns the lines of the Subject header without the name of
// the header and without the space of a continuation line.
func subjectLines(t *testing.T, header []string) []string {
	t.Helper()
	var lines []string
	inSubject := false
	for _, line := range header {
		switch {
		case strings.HasPrefix(line, "Subject: "):
			inSubject = true
			lines = append(lines, strings.TrimPrefix(line, "Subject: "))
		case inSubject && strings.HasPrefix(line, " "):
			lines = append(lines, strings.TrimPrefix(line, " "))
		default:
			inSubject = false
			// Only the subject has continuation lines.
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || !strings.Contains(line, ": ") {
				t.Errorf("unexpected header line %q", line)
			}
		}
	}
	return lines
}

// Each encoded word of a long subject goes on its own line, and a reader gets
// the original text back.
func TestBuildFoldsALongSubject(t *testing.T) {
	from := &netmail.Address{Address: "tree@example.org"}
	for _, subject := range longSubjects {
		m := Message{To: "anna@example.org", Subject: subject, Text: longText}
		if err := checkMessage(m); err != nil {
			t.Fatalf("%q: %v", subject, err)
		}
		raw := Build(from, m, testTime)
		header, _ := splitLetter(t, raw)
		words := subjectLines(t, header)
		if len(words) < 2 {
			t.Errorf("%q: the subject has %d lines, want several", subject, len(words))
		}
		for _, word := range words {
			// RFC 2047 limits an encoded word to 75 characters.
			if !strings.HasPrefix(word, "=?utf-8?q?") || !strings.HasSuffix(word, "?=") || strings.Contains(word, " ") || len(word) > 75 {
				t.Errorf("%q: the line %q is not one encoded word", subject, word)
			}
		}
		for _, line := range header {
			// A continuation line holds one space and one encoded word.
			if strings.HasPrefix(line, " ") && len(line) > 76 {
				t.Errorf("%q: continuation line of %d characters", subject, len(line))
			}
		}
		// A reader joins the lines and decodes the words.
		decoder := new(mime.WordDecoder)
		got, err := decoder.DecodeHeader(strings.Join(words, " "))
		if err != nil || got != subject {
			t.Errorf("subject after the decoder: %q %v, want %q", got, err, subject)
		}
		letter, err := netmail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%q: %v", subject, err)
		}
		got, err = decoder.DecodeHeader(letter.Header.Get("Subject"))
		if err != nil || got != subject {
			t.Errorf("subject after the parser: %q %v, want %q", got, err, subject)
		}
		// The fold adds no header.
		if n := len(letter.Header); n != 9 {
			t.Errorf("%q: %d headers, want 9", subject, n)
		}
	}
}

// RFC 5322, section 2.1.1: a line should have no more than 78 characters.
func TestBuildHeaderLinesFitIn78Characters(t *testing.T) {
	// BUG: internal/mail/mail.go:200. Build puts the first encoded word on the
	// line of the header name. mime.QEncoding makes words of up to 75
	// characters, so the first line has up to 84 characters ("Subject: " and
	// the word). The subject of the real Russian code letter gives a first
	// line of 83 characters. Only the continuation lines fit.
	// DOUBT: internal/mail/mail.go:197. The From header is not folded at all:
	// a sender name of 44 Cyrillic letters gives one line of 310 characters.
	// A long plain ASCII subject is not folded either. All of them stay below
	// the hard limit of 998 characters.
	plain := &netmail.Address{Address: "tree@example.org"}
	named := &netmail.Address{Name: "Семейное древо нашей большой и дружной семьи", Address: "tree@example.org"}
	subjects := append([]string{strings.Repeat("An invitation to the family tree with a long plain subject. ", 5) + "End"}, longSubjects...)
	for _, from := range []*netmail.Address{plain, named} {
		for _, subject := range subjects {
			raw := Build(from, Message{To: "anna@example.org", Subject: subject, Text: longText}, testTime)
			header, _ := splitLetter(t, raw)
			for _, line := range header {
				if len(line) > 78 {
					t.Errorf("header line of %d characters: %q", len(line), line)
				}
			}
			letter, err := netmail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := new(mime.WordDecoder).DecodeHeader(letter.Header.Get("Subject"))
			if err != nil || got != subject {
				t.Errorf("subject: %q %v, want %q", got, err, subject)
			}
			parsed, err := letter.Header.AddressList("From")
			if err != nil || len(parsed) != 1 || *parsed[0] != *from {
				t.Errorf("From: %+v %v", parsed, err)
			}
		}
	}
}

func TestBuildShortAndEmptyBody(t *testing.T) {
	for _, text := range []string{"", "a", "Your code:\n\n    1234 5678\n", strings.Repeat("x", 57), strings.Repeat("x", 58), strings.Repeat("я", 500)} {
		m := Message{To: "anna@example.org", Subject: "Sign-in code", Text: text}
		raw := Build(&netmail.Address{Address: "tree@example.org"}, m, testTime)
		_, body := splitLetter(t, raw)
		for _, line := range body {
			if len(line) > 76 {
				t.Errorf("%d characters of text: body line of %d characters", len(text), len(line))
			}
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(body, ""))
		if err != nil || string(decoded) != text {
			t.Errorf("%d characters of text: body %q %v", len(text), decoded, err)
		}
		letter, err := netmail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if got := letter.Header.Get("Subject"); got != "Sign-in code" {
			t.Errorf("a plain subject changed: %q", got)
		}
	}
}

func TestParseFrom(t *testing.T) {
	for text, want := range map[string]netmail.Address{
		"tree@example.org":                  {Address: "tree@example.org"},
		"Family Tree <tree@example.org>":    {Name: "Family Tree", Address: "tree@example.org"},
		"Семейное древо <tree@example.org>": {Name: "Семейное древо", Address: "tree@example.org"},
	} {
		got, err := ParseFrom(text)
		if err != nil || *got != want {
			t.Errorf("%q: %+v %v", text, got, err)
		}
	}
	for _, text := range []string{
		"", "tree", "Family Tree", "tree@example.org\r\nBcc: other@example.com", "tree@example.org\n", "\rtree@example.org",
		"a@example.org, b@example.org",
	} {
		if got, err := ParseFrom(text); err == nil {
			t.Errorf("%q accepted as %+v", text, got)
		}
	}
}

func TestIsLocalHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "127.8.9.10": true, "::1": true,
		"example.org": false, "192.0.2.1": false, "localhost.example.org": false, "127.0.0.1.example.org": false, "": false,
	} {
		if got := isLocalHost(host); got != want {
			t.Errorf("%q: got %v", host, got)
		}
	}
}

// badMessages have a line break in a header or no usable recipient.
var badMessages = map[string]Message{
	"LF in To":              {To: "anna@example.org\nBcc: other@example.com", Subject: "s"},
	"CR LF in To":           {To: "anna@example.org\r\nBcc: other@example.com", Subject: "s"},
	"CR in To":              {To: "anna@example.org\r", Subject: "s"},
	"LF in Subject":         {To: "anna@example.org", Subject: "s\nBcc: other@example.com"},
	"CR LF in Subject":      {To: "anna@example.org", Subject: "s\r\nBcc: other@example.com"},
	"CR in Subject":         {To: "anna@example.org", Subject: "s\r"},
	"empty To":              {To: "", Subject: "s"},
	"To is not an address":  {To: "anna", Subject: "s"},
	"To with a name":        {To: "Anna <anna@example.org>", Subject: "s"},
	"To with a quoted name": {To: `"Anna" <anna@example.org>`, Subject: "s"},
	"To in angle brackets":  {To: "<anna@example.org>", Subject: "s"},
	"To with a comment":     {To: "anna@example.org (Anna)", Subject: "s"},
	"To with spaces":        {To: " anna@example.org ", Subject: "s"},
	"two addresses in To":   {To: "anna@example.org, other@example.com", Subject: "s"},
}

func TestCheckMessage(t *testing.T) {
	if err := checkMessage(testMessage()); err != nil {
		t.Fatalf("a good message: %v", err)
	}
	// A line break in the text is normal.
	if err := checkMessage(Message{To: "anna@example.org", Subject: "s", Text: "a\r\nb\n"}); err != nil {
		t.Fatalf("a text with line breaks: %v", err)
	}
	for name, m := range badMessages {
		err := checkMessage(m)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		// The error goes to the log, so it must not repeat the input.
		if strings.Contains(err.Error(), "example") {
			t.Errorf("%s: the error holds the input: %v", name, err)
		}
	}
}

func TestSendersRefuseBadMessagesBeforeTheNetwork(t *testing.T) {
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer api.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			calls.Add(1)
			_ = conn.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	senders := []Sender{
		&Resend{Key: "re_test_key", From: testFrom, Endpoint: api.URL},
		&SMTP{Host: "127.0.0.1", Port: port, From: testFrom, Timeout: 2 * time.Second},
	}
	for _, sender := range senders {
		for name, m := range badMessages {
			if err := sender.Send(context.Background(), m); err == nil {
				t.Errorf("%s, %s: sent", sender.Name(), name)
			}
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("a refused message opened %d connections", n)
	}
}

func TestSenderNames(t *testing.T) {
	for want, sender := range map[string]Sender{"log": Log{}, "smtp": &SMTP{}, "resend": &Resend{}} {
		if got := sender.Name(); got != want {
			t.Errorf("name %q, want %q", got, want)
		}
	}
}

func TestLogWritesTheCodeAndNotTheAddress(t *testing.T) {
	var out bytes.Buffer
	sender := Log{Logger: slog.New(slog.NewTextHandler(&out, nil))}
	if err := sender.Send(context.Background(), testMessage()); err != nil {
		t.Fatal(err)
	}
	line := out.String()
	for _, want := range []string{"code=12345678", "user=42", "kind=code"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log lacks %s: %s", want, line)
		}
	}
	for _, secret := range []string{"anna", "example.org", "@", "семейное", "1234 5678"} {
		if strings.Contains(line, secret) {
			t.Errorf("the log holds %q: %s", secret, line)
		}
	}

	out.Reset()
	invite := Message{To: "boris@example.org", Subject: "Invitation to the family tree", Text: "Anna invites you.", Kind: "invite", UserID: 7}
	if err := sender.Send(context.Background(), invite); err != nil {
		t.Fatal(err)
	}
	line = out.String()
	if !strings.Contains(line, "user=7") || !strings.Contains(line, "kind=invite") {
		t.Errorf("the log lacks the account or the kind: %s", line)
	}
	for _, secret := range []string{"boris", "example.org", "@", "code=", "Anna invites"} {
		if strings.Contains(line, secret) {
			t.Errorf("the log holds %q: %s", secret, line)
		}
	}
}

func TestResendSend(t *testing.T) {
	const key = "re_test"
	type call struct {
		method, path, authorization, contentType string
		body                                     []byte
	}
	calls := make(chan call, 4)
	status := http.StatusOK
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls <- call{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body}
		w.WriteHeader(status)
		// A service can repeat the key in its answer. It must not reach the error.
		_, _ = w.Write([]byte(`{"message":"API key ` + key + ` is invalid"}`))
	}))
	defer api.Close()
	sender := &Resend{Key: key, From: testFrom, Endpoint: api.URL + "/emails"}
	m := testMessage()
	if err := sender.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	got := <-calls
	if got.method != http.MethodPost || got.path != "/emails" || got.authorization != "Bearer "+key || got.contentType != "application/json" {
		t.Fatalf("request: %s %s auth=%q type=%q", got.method, got.path, got.authorization, got.contentType)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("%v: %s", err, got.body)
	}
	to, _ := payload["to"].([]any)
	if len(payload) != 4 || len(to) != 1 || to[0] != m.To || payload["subject"] != m.Subject || payload["text"] != m.Text {
		t.Fatalf("payload: %s", got.body)
	}
	// The API gets the name as text, not as the codes of a mail header.
	if payload["from"] != "Семейное древо <tree@example.org>" {
		t.Fatalf("from: %v", payload["from"])
	}
	// The code and the account number are only for the log transport.
	if bytes.Contains(got.body, []byte(m.Code)) {
		t.Fatalf("the payload holds the code as a field: %s", got.body)
	}

	for _, code := range []int{300, 400, 401, 403, 422, 429, 500, 503} {
		status = code
		err := sender.Send(context.Background(), m)
		<-calls
		if err == nil {
			t.Errorf("status %d: no error", code)
			continue
		}
		if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), m.To) {
			t.Errorf("status %d: the error holds a secret: %v", code, err)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(code)) {
			t.Errorf("status %d: the error does not name the status: %v", code, err)
		}
	}
	for _, code := range []int{200, 201, 202, 299} {
		status = code
		if err := sender.Send(context.Background(), m); err != nil {
			t.Errorf("status %d: %v", code, err)
		}
		<-calls
	}
}

func TestResendDoesNotFollowRedirects(t *testing.T) {
	const key = "re_test"
	var followed atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer api.Close()
	err := (&Resend{Key: key, From: testFrom, Endpoint: api.URL}).Send(context.Background(), testMessage())
	if err == nil {
		t.Fatal("a redirect counts as a sent letter")
	}
	if followed.Load() != 0 {
		t.Fatal("the client carried the key to another server")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the error holds the key: %v", err)
	}
}

func TestResendNetworkErrorHidesTheKey(t *testing.T) {
	const key = "re_test"
	api := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := api.URL
	api.Close()
	err := (&Resend{Key: key, From: testFrom, Endpoint: endpoint}).Send(context.Background(), testMessage())
	if err == nil {
		t.Fatal("no error from a closed server")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "anna@example.org") {
		t.Fatalf("the error holds a secret: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Resend{Key: key, From: testFrom, Endpoint: endpoint}).Send(ctx, testMessage()); err == nil {
		t.Fatal("no error with a cancelled context")
	}
}

// smtpSession is what the fake SMTP server saw.
type smtpSession struct {
	commands []string
	data     []byte
}

// fakeSMTP serves one plain SMTP session on the loopback interface. It offers
// no STARTTLS, which the client permits only for a server on the same computer.
func fakeSMTP(t *testing.T, rcptAnswer string) (port string, done <-chan smtpSession) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	out := make(chan smtpSession, 1)
	go func() {
		var session smtpSession
		defer func() { out <- session }()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		reader := bufio.NewReader(conn)
		say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
		say("220 localhost ESMTP fake")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			session.commands = append(session.commands, line)
			switch verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0]); verb {
			case "EHLO":
				say("250-localhost")
				say("250 AUTH PLAIN")
			case "AUTH":
				say("235 2.7.0 accepted")
			case "MAIL":
				say("250 2.1.0 ok")
			case "RCPT":
				say(rcptAnswer)
			case "DATA":
				say("354 go ahead")
				for {
					part, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					if string(part) == ".\r\n" {
						break
					}
					session.data = append(session.data, part...)
				}
				say("250 2.0.0 queued")
			case "QUIT":
				say("221 2.0.0 bye")
				return
			default:
				say("502 5.5.1 not implemented")
			}
		}
	}()
	_, port, err = net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port, out
}

func TestSMTPSend(t *testing.T) {
	port, done := fakeSMTP(t, "250 2.1.5 ok")
	sender := &SMTP{Host: "127.0.0.1", Port: port, User: "mailer", Password: "smtp-password", From: testFrom, Timeout: 5 * time.Second}
	m := testMessage()
	if err := sender.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	session := <-done
	commands := strings.Join(session.commands, "\n")
	wantAuth := "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00mailer\x00smtp-password"))
	for _, want := range []string{wantAuth, "MAIL FROM:<tree@example.org>", "RCPT TO:<anna@example.org>", "DATA", "QUIT"} {
		if !strings.Contains(commands, want) {
			t.Errorf("the server did not get %q:\n%s", want, commands)
		}
	}
	_, body := splitLetter(t, session.data)
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(body, ""))
	if err != nil || string(decoded) != m.Text {
		t.Fatalf("body: %q %v", decoded, err)
	}
	letter, err := netmail.ReadMessage(bytes.NewReader(session.data))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(letter.Header.Get("Subject"))
	if err != nil || subject != m.Subject || letter.Header.Get("To") != "<anna@example.org>" {
		t.Fatalf("subject %q, To %q, %v", subject, letter.Header.Get("To"), err)
	}
	if date, err := letter.Header.Date(); err != nil || time.Since(date) > time.Minute || time.Since(date) < -time.Minute {
		t.Fatalf("Date: %v %v", date, err)
	}
}

func TestSMTPRefusedRecipient(t *testing.T) {
	port, done := fakeSMTP(t, "550 5.1.1 no such user")
	sender := &SMTP{Host: "127.0.0.1", Port: port, From: testFrom, Timeout: 5 * time.Second}
	err := sender.Send(context.Background(), testMessage())
	if err == nil || !strings.Contains(err.Error(), "recipient refused") {
		t.Fatalf("want a refused recipient, got %v", err)
	}
	session := <-done
	if len(session.data) != 0 {
		t.Fatalf("the letter went out after the refusal: %q", session.data)
	}
	// Without a user name the client does not sign in to the server.
	if strings.Contains(strings.Join(session.commands, "\n"), "AUTH") {
		t.Fatalf("the client signed in without a user: %v", session.commands)
	}
}

func TestSMTPTimeout(t *testing.T) {
	// This server accepts the connection and then says nothing.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	held := make(chan net.Conn, 1)
	go func() {
		if conn, err := listener.Accept(); err == nil {
			held <- conn
		}
	}()
	defer func() {
		select {
		case conn := <-held:
			_ = conn.Close()
		default:
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	sender := &SMTP{Host: "127.0.0.1", Port: port, From: testFrom, Timeout: 200 * time.Millisecond}
	start := time.Now()
	if err := sender.Send(context.Background(), testMessage()); err == nil {
		t.Fatal("no error from a silent server")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the timeout did not work: %s", elapsed)
	}
}

func TestPlainFrom(t *testing.T) {
	for text, want := range map[string]string{
		"tree@example.org":          "tree@example.org",
		"Родник <tree@example.org>": "Родник <tree@example.org>",
		"Родник — семейное древо <tree@example.org>": "Родник — семейное древо <tree@example.org>",
		`"Orlov, family" <tree@example.org>`:         `"Orlov, family" <tree@example.org>`,
		`"The \"Tree\"" <tree@example.org>`:          `"The \"Tree\"" <tree@example.org>`,
	} {
		from, err := ParseFrom(text)
		if err != nil {
			t.Errorf("%s: %v", text, err)
			continue
		}
		got := plainFrom(from)
		if got != want {
			t.Errorf("%s: got %s, want %s", text, got, want)
		}
		// The text names the same sender when a mail parser reads it.
		if again, err := netmail.ParseAddress(got); err != nil || *again != *from {
			t.Errorf("%s: read back as %v (%v)", got, again, err)
		}
	}
}
