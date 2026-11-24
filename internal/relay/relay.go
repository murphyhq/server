package relay

import (
	"context"
	"net/http"
	"time"

	"github.com/murphyhq/server/internal/httpclient"
)

const timeout = 10 * time.Second

func New(url, key string) (*ClientWithResponses, error) {
	return NewClientWithResponses(url,
		WithHTTPClient(httpclient.New(timeout)),
		WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set("Authorization", "Bearer "+key)
			return nil
		}),
	)
}
