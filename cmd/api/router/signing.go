package router

import (
	"context"

	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications/webhook"
)

func (s *Server) GetSigningKeys(ctx context.Context, _ GetSigningKeysRequestObject) (_ GetSigningKeysResponseObject, err error) {
	_, span := tracer.Span(ctx)
	defer span.End(&err)

	keys := make([]SigningKey, len(s.publicKeys))
	for i, key := range s.publicKeys {
		keys[i] = SigningKey{PublicKey: webhook.EncodePublicKey(key)}
	}
	return GetSigningKeys200JSONResponse{Keys: keys}, nil
}
