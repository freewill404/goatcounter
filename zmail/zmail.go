// Package zmail is a simple mail sender.
package zmail

import (
	"crypto/rand"
	"fmt"
	"hash/fnv"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"zgo.at/zlog"
)

var (
	SMTP  = ""   // SMTP server connection string.
	Print = true // Print emails to stdout if SMTP if empty.
)

// Send an email.
func Send(subject string, from mail.Address, to []mail.Address, body string) error {
	msg := format(subject, from, to, body)

	// Just print to stdout and return.
	if SMTP == "stdout" && Print {
		l := strings.Repeat("═", 50)
		fmt.Println("╔═══ EMAIL " + l + "\n║ " +
			strings.Replace(strings.TrimSpace(string(msg)), "\r\n", "\r\n║ ", -1) +
			"\n╚══════════" + l + "\n")
		return nil
	}

	toList := make([]string, len(to))
	for i := range to {
		toList[i] = to[i].Address
	}

	if SMTP == "" {
		return sendMail(subject, from, toList, msg)
	}

	return sendRelay(subject, from, toList, msg)
}

var hostname string

// Send direct.
func sendMail(subject string, from mail.Address, to []string, body []byte) error {
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("cannot determine this system's hostname: %w", err)
		}
	}

	go func() {
		for _, t := range to {
			domain := t[strings.LastIndex(t, "@")+1:]
			mxs, err := net.LookupMX(domain)
			if err != nil {
				zlog.Field("domain", domain).Errorf("zmail sendMail: %s", err)
				return
			}

		mxloop:
			for _, mx := range mxs {
				for _, port := range []string{"25", "465", "587"} {
					err := smtp.SendMail(mx.Host+":"+port, nil, from.Address, to, body)
					if err != nil {
						zlog.Fields(zlog.F{
							"host": mx.Host,
							"from": from,
							"to":   to,
						}).Error(errors.Wrap(err, "smtp.SendMail"))
					}

					if err == nil {
						break mxloop
					}
				}
			}
		}
	}()
	return nil
}

// Send via relay.
func sendRelay(subject string, from mail.Address, to []string, body []byte) error {
	srv, err := url.Parse(SMTP)
	if err != nil {
		return err
	}

	user := srv.User.Username()
	pw, _ := srv.User.Password()
	host := srv.Host
	if h, _, err := net.SplitHostPort(srv.Host); err == nil {
		host = h
	}

	go func() {
		var auth smtp.Auth
		if user != "" {
			auth = smtp.PlainAuth("", user, pw, host)
		}

		err := smtp.SendMail(srv.Host, auth, from.Address, to, body)
		if err != nil {
			zlog.Fields(zlog.F{
				"host": srv.Host,
				"from": from,
				"to":   to,
			}).Error(errors.Wrap(err, "smtp.SendMail"))
		}
	}()
	return nil
}

var reNL = regexp.MustCompile(`(\r\n){2,}`)

// format a message.
func format(subject string, from mail.Address, to []mail.Address, body string) []byte {
	var msg strings.Builder
	t := time.Now()

	fmt.Fprintf(&msg, "From: %s\r\n", from.String())

	tos := make([]string, len(to))
	for i := range to {
		tos[i] = to[i].String()
	}
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(tos, ","))

	// Create Message-ID
	domain := from.Address[strings.Index(from.Address, "@")+1:]
	hash := fnv.New64a()
	hash.Write([]byte(body))
	rnd, _ := rand.Int(rand.Reader, big.NewInt(0).SetUint64(18446744073709551615))
	msgid := fmt.Sprintf("zmail-%s-%s@%s", strconv.FormatUint(hash.Sum64(), 36),
		strconv.FormatUint(rnd.Uint64(), 36), domain)

	fmt.Fprintf(&msg, "Date: %s\r\n", t.Format(time.RFC1123Z))
	fmt.Fprintf(&msg, "Content-Type: text/plain;charset=utf-8\r\n")
	fmt.Fprintf(&msg, "Content-Transfer-Encoding: quoted-printable\r\n")
	fmt.Fprintf(&msg, "Message-ID: <%s>\r\n", msgid)
	fmt.Fprintf(&msg, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", reNL.ReplaceAllString(subject, "")))
	msg.WriteString("\r\n")

	w := quotedprintable.NewWriter(&msg)
	w.Write([]byte(body))
	w.Close()

	return []byte(msg.String())
}
