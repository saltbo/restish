package cli

import (
	"context"
	"errors"
	"net/http"

	"github.com/saltbo/restish/v2/internal/output"
)

// ResponseMiddleware inspects a normalized response before Restish renders it.
// Returning a nil Response preserves the current response.
type ResponseMiddleware func(context.Context, *http.Request, *output.Response) (ResponseMiddlewareResult, error)

// ResponseMiddlewareResult replaces or suppresses a normalized response.
type ResponseMiddlewareResult struct {
	Response *output.Response
	Drop     bool
}

func (c *CLI) runResponseMiddlewares(ctx context.Context, request *http.Request, response *output.Response) (*output.Response, bool, error) {
	current := response
	for _, middleware := range c.responseMiddlewares {
		result, err := middleware(ctx, request, current)
		if err != nil {
			return nil, false, err
		}
		if result.Drop && result.Response != nil {
			return nil, false, errors.New("response middleware cannot both replace and drop a response")
		}
		if result.Drop {
			return nil, true, nil
		}
		if result.Response != nil {
			current = result.Response
		}
	}
	return current, false, nil
}
