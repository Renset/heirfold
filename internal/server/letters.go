package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/mail"
	"github.com/Renset/heirfold/internal/store"
)

// letterLang returns the language of a letter: the wanted one if Heirfold has
// it, or English.
func letterLang(lang string) string {
	if lang == "ru" {
		return "ru"
	}
	return "en"
}

func validLang(lang string) bool { return lang == "ru" || lang == "en" }

// spacedCode splits a code into two groups, which is easier to read and type.
func spacedCode(code string) string {
	half := len(code) / 2
	return code[:half] + " " + code[half:]
}

// safeInviter returns the name of the inviter for a letter, or an empty text
// if the name looks like an address or a link. A letter from the server must
// not carry a link that an editor chose.
func safeInviter(name string) string {
	lower := strings.ToLower(name)
	if strings.ContainsAny(name, ":/@<>\\") || strings.Contains(lower, "www") {
		return ""
	}
	for i := 0; i+1 < len(name); i++ {
		if name[i] == '.' && name[i+1] != ' ' {
			return ""
		}
	}
	return name
}

func codeLetter(lang, code, site string) (subject, text string) {
	if letterLang(lang) == "ru" {
		return "Код для входа в семейное древо",
			"Код для входа в семейное древо:\n\n    " + spacedCode(code) + "\n\n" +
				"Код действует 10 минут. Введите его на странице, где вы запросили вход.\n\n" +
				"Если вы не запрашивали код, ничего не делайте. Без кода никто не войдёт.\n\n" + site + "\n"
	}
	return "Your sign-in code for the family tree",
		"Your sign-in code for the family tree:\n\n    " + spacedCode(code) + "\n\n" +
			"The code works for 10 minutes. Type it on the page where you asked to sign in.\n\n" +
			"If you did not ask for a code, do nothing. Nobody can sign in without the code.\n\n" + site + "\n"
}

func inviteLetter(lang, inviter, site string) (subject, text string) {
	if letterLang(lang) == "ru" {
		first := "Вас приглашают в семейное древо."
		if inviter != "" {
			first = inviter + " приглашает вас в семейное древо."
		}
		return "Приглашение в семейное древо",
			first + "\n\nОткройте сайт:\n" + site + "\n\n" +
				"Введите на нём этот адрес почты. Пароль не нужен: сайт пришлёт вам код для входа.\n"
	}
	first := "You have an invitation to the family tree."
	if inviter != "" {
		first = inviter + " invites you to the family tree."
	}
	return "Invitation to the family tree",
		first + "\n\nOpen the site:\n" + site + "\n\n" +
			"Type this e-mail address there. You need no password: the site sends you a sign-in code.\n"
}

// background runs a job after the response. It limits how many jobs run at
// the same time, and it drops a job when the server is too busy.
func (s *Server) background(job func(ctx context.Context)) {
	select {
	case s.jobs <- struct{}{}:
	default:
		s.log.Warn("background job dropped: the server is busy")
		return
	}
	go func() {
		defer func() { <-s.jobs }()
		defer func() {
			if recover() != nil {
				s.log.Error("panic in background job")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		job(ctx)
	}()
}

// mailAllowed takes one letter from the allowance of the server. The letters
// to known browsers, the letters to strangers and the invitations have
// separate allowances, so a flood of one kind does not stop the others.
func (s *Server) mailAllowed(pool string, m mail.Message) bool {
	if s.anon.allow("mail-"+pool, ruleMailGlobal) {
		return true
	}
	s.log.Warn("letter not sent: too many letters in a short time", "kind", m.Kind, "user", m.UserID)
	return false
}

// send delivers one letter. An account whose name is not an address cannot
// get a letter, so its code goes to the server log.
func (s *Server) send(ctx context.Context, m mail.Message) {
	sender := s.mail
	if !auth.ValidEmail(m.To) {
		sender = s.logMail
	}
	if err := sender.Send(ctx, m); err != nil {
		// A mail server can repeat the address in its answer.
		text := strings.ReplaceAll(err.Error(), m.To, "<address>")
		s.log.Error("letter not sent", "kind", m.Kind, "user", m.UserID, "transport", sender.Name(), "error", text)
	}
}

// sendCode creates a sign-in code for a user and sends it.
// A code that cannot go out is not stored, so nobody can guess it.
func (s *Server) sendCode(ctx context.Context, user *store.User, attempt, lang string, trusted bool) {
	pool := "strangers"
	if trusted {
		pool = "known"
	}
	if !s.mailAllowed(pool, mail.Message{Kind: "code", UserID: user.ID}) {
		return
	}
	code, err := auth.NewCode()
	if err != nil {
		s.log.Error("sign-in code failed", "error", err)
		return
	}
	err = s.store.CreateLoginCode(ctx, auth.HashToken(attempt), user.ID, user.Email, auth.HashCode(attempt, code), codeLifetime)
	if errors.Is(err, store.ErrNotFound) {
		// The address or the role changed after the request.
		return
	}
	if err != nil {
		s.log.Error("sign-in code failed", "error", err)
		return
	}
	// The letter uses the language of the account. The language of the page
	// serves only an account that has no language yet: a browser in another
	// language, or a stranger who asks for a code, must not change what the
	// owner reads.
	if validLang(user.Lang) {
		lang = user.Lang
	}
	subject, text := codeLetter(lang, code, s.cfg.BaseURL)
	s.send(ctx, mail.Message{To: user.Email, Subject: subject, Text: text, Kind: "code", UserID: user.ID, Code: code})
}

// sendInvite tells a person that an account waits for them.
func (s *Server) sendInvite(ctx context.Context, user *store.User, inviter string) {
	if !s.mailAllowed("invites", mail.Message{Kind: "invite", UserID: user.ID}) {
		return
	}
	subject, text := inviteLetter(user.Lang, safeInviter(inviter), s.cfg.BaseURL)
	s.send(ctx, mail.Message{To: user.Email, Subject: subject, Text: text, Kind: "invite", UserID: user.ID})
}
