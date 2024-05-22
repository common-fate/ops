package tunnel

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/common-fate/ops/protocol"
)

const authorizationMetadataKey = "Authorization"

// Authenticator is a type which adds authentication credentials to an outbound
// register listener request.
// It is called before the request is serialized and written to the stream.
type Authenticator interface {
	Authenticate(context.Context, *protocol.RegisterListenerRequest) error
}

// AuthenticatorFunc is a function which implements the Authenticator interface
type AuthenticatorFunc func(context.Context, *protocol.RegisterListenerRequest) error

// Authenticate delegates to the underlying AuthenticatorFunc
func (a AuthenticatorFunc) Authenticate(ctx context.Context, r *protocol.RegisterListenerRequest) error {
	return a(ctx, r)
}

var defaultAuthenticator Authenticator = AuthenticatorFunc(func(ctx context.Context, rlr *protocol.RegisterListenerRequest) error {
	slog.Warn("No authenticator provided, attempting to register connection without credentials")
	return nil
})

// BearerAuthenticator returns an instance of Authenticator which configures Bearer authentication
// on requests passed to Authenticate using the provided token string
func BearerAuthenticator(token string) Authenticator {
	return AuthenticatorFunc(func(ctx context.Context, rlr *protocol.RegisterListenerRequest) error {
		if rlr.Metadata == nil {
			rlr.Metadata = map[string]string{}
		}

		rlr.Metadata[authorizationMetadataKey] = fmt.Sprintf("%s %s", "Bearer", token)

		return nil
	})
}
