package nats

import (
	"errors"
	"fmt"
	"net/url"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	URL string
}

func Open(cfg Config) (*gonats.Conn, jetstream.JetStream, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" {
		return nil, nil, errors.New("not a valid NATS URL")
	}
	u.Path, u.RawQuery = "", ""

	conn, err := gonats.Connect(u.String(), gonats.MaxReconnects(-1))
	if err != nil {
		return nil, nil, fmt.Errorf("connect to NATS: %w", err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("open JetStream: %w", err)
	}
	return conn, js, nil
}
