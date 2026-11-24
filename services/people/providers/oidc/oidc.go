package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpclient"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/people"
)

const timeout = 10 * time.Second

type Config struct {
	Issuer         string
	BackchannelURL string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
}

type Provider struct {
	config     Config
	client     context.Context
	mu         sync.Mutex
	discovered *discovered
}

type discovered struct {
	oauth    *oauth2.Config
	verifier *gooidc.IDTokenVerifier
}

var _ people.Provider = (*Provider)(nil)

func New(config Config) *Provider {
	return &Provider{config: config, client: gooidc.ClientContext(context.Background(), httpclient.New(timeout))}
}

func (p *Provider) SignInURL(ctx context.Context, state, nonce, verifier string) (_ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	return d.oauth.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

func (p *Provider) Exchange(ctx context.Context, code, verifier string) (_ *people.Claims, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	d, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	token, err := d.oauth.Exchange(p.withClient(ctx), code, oauth2.VerifierOption(verifier))
	var rejected *oauth2.RetrieveError
	if errors.As(err, &rejected) {
		return nil, fmt.Errorf("%w: the identity provider refused the sign-in", murphy.ErrUnauthenticated)
	}
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}
	idToken, _ := token.Extra("id_token").(string)
	return p.verify(ctx, d, idToken)
}

func (p *Provider) Verify(ctx context.Context, idToken string) (_ *people.Claims, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	d, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	return p.verify(ctx, d, idToken)
}

func (p *Provider) verify(ctx context.Context, d *discovered, raw string) (*people.Claims, error) {
	token, err := d.verifier.Verify(p.withClient(ctx), raw)
	if err != nil {
		return nil, fmt.Errorf("%w: the identity token is not valid", murphy.ErrUnauthenticated)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: the identity token has unreadable claims", murphy.ErrUnauthenticated)
	}
	return &people.Claims{
		Issuer: token.Issuer, Subject: token.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified,
		Name: claims.Name, Nonce: token.Nonce,
	}, nil
}

func (p *Provider) discover(ctx context.Context) (*discovered, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovered != nil {
		return p.discovered, nil
	}
	provider, err := gooidc.NewProvider(gooidc.InsecureIssuerURLContext(p.withClient(ctx), p.config.Issuer), p.backchannel(p.config.Issuer))
	if err != nil {
		return nil, fmt.Errorf("discover the identity provider: %w", err)
	}
	var metadata struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("read the identity provider's metadata: %w", err)
	}
	endpoint := provider.Endpoint()
	endpoint.TokenURL = p.backchannel(endpoint.TokenURL)
	keys := gooidc.NewRemoteKeySet(p.client, p.backchannel(metadata.JWKSURL))
	p.discovered = &discovered{
		oauth: &oauth2.Config{
			ClientID: p.config.ClientID, ClientSecret: p.config.ClientSecret, RedirectURL: p.config.RedirectURL,
			Endpoint: endpoint, Scopes: []string{gooidc.ScopeOpenID, "email", "profile"},
		},
		verifier: gooidc.NewVerifier(p.config.Issuer, keys, &gooidc.Config{ClientID: p.config.ClientID}),
	}
	return p.discovered, nil
}

func (p *Provider) backchannel(url string) string {
	if p.config.BackchannelURL == "" {
		return url
	}
	rest, ok := strings.CutPrefix(url, strings.TrimSuffix(p.config.Issuer, "/"))
	if !ok {
		return url
	}
	return strings.TrimSuffix(p.config.BackchannelURL, "/") + rest
}

func (p *Provider) withClient(ctx context.Context) context.Context {
	return gooidc.ClientContext(ctx, httpclient.New(timeout))
}
