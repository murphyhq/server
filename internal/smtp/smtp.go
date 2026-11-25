package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	netsmtp "net/smtp"
	"net/textproto"
	"strconv"
	"time"

	"github.com/murphyhq/server/internal/tracer"
)

const Timeout = 30 * time.Second

const implicitTLSPort = 465

type Server struct {
	Host     string
	Port     int
	Username string
	Password string
	Roots    *x509.CertPool
}

func (s Server) Check(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.Quit()
}

func (s Server) Send(ctx context.Context, from, to string, message []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("the mail server refused the sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("the mail server refused the recipient: %w", err)
	}
	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("the mail server refused the message: %w", err)
	}
	if _, err := body.Write(message); err != nil {
		return fmt.Errorf("write the message: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("the mail server refused the message: %w", err)
	}
	return client.Quit()
}

func Permanent(err error) bool {
	var reply *textproto.Error
	return errors.As(err, &reply) && reply.Code >= 500
}

func (s Server) connect(ctx context.Context) (*netsmtp.Client, error) {
	address := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	config := &tls.Config{ServerName: s.Host, RootCAs: s.Roots, MinVersion: tls.VersionTLS12}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var dialer net.Dialer
	var conn net.Conn
	var err error
	if s.Port == implicitTLSPort {
		conn, err = (&tls.Dialer{NetDialer: &dialer, Config: config}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}
	if err := conn.SetDeadline(time.Now().Add(Timeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}
	client, err := netsmtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("greet %s: %w", address, err)
	}
	if err := s.secure(client, config); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (s Server) secure(client *netsmtp.Client, config *tls.Config) error {
	if s.Port != implicitTLSPort {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("the mail server doesn't offer STARTTLS, and Murphy only sends email over TLS")
		}
		if err := client.StartTLS(config); err != nil {
			return fmt.Errorf("start TLS: %w", err)
		}
	}
	if s.Username == "" {
		return nil
	}
	if ok, _ := client.Extension("AUTH"); !ok {
		return errors.New("the mail server doesn't offer authentication")
	}
	if err := client.Auth(netsmtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
		return fmt.Errorf("sign in to the mail server: %w", err)
	}
	return nil
}
