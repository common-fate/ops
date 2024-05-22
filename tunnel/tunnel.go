package tunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/common-fate/ops/protocol"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	DefaultTLSConfig = &tls.Config{
		NextProtos: []string{protocol.Name},
	}

	DefaultQuicConfig = &quic.Config{
		MaxIdleTimeout:  20 * time.Second,
		KeepAlivePeriod: 10 * time.Second,
	}

	DefaultBackoff = wait.Backoff{
		Steps:    5,
		Duration: 100 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
	}
)

type Tunnel struct {
	Namespace         string
	Handler           http.Handler
	Logger            *slog.Logger
	TLSConfig         *tls.Config
	QuicConfig        *quic.Config
	Authenticator     Authenticator
	OnConnectionReady func(protocol.RegisterListenerResponse)
}

func coallesce[T any](v, d *T) *T {
	if v == nil {
		return d
	}

	return v
}

func (s *Tunnel) getTLSConfig(addr string) (*tls.Config, error) {
	tlsConf := s.TLSConfig
	if tlsConf == nil {
		tlsConf = DefaultTLSConfig
	}
	if tlsConf.ServerName == "" {
		// if the TLS ServerName is not explicitly supplied
		// then we will parse the dial address and use the hostname
		// defined on that instead
		url, err := url.Parse(addr)
		if err != nil {
			return nil, err
		}

		tlsConf.ServerName = url.Hostname()
	}

	return tlsConf, nil
}

func (s *Tunnel) DialAndServe(ctx context.Context, addr string) (err error) {
	attrs := []slog.Attr{slog.String("addr", addr)}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		attrs = []slog.Attr{slog.String("host", host), slog.String("port", port)}
	}

	log := slog.New(coallesce(s.Logger, slog.Default()).Handler().WithAttrs(attrs))
	log.Debug("Dialing address")

	var lastErr error
	err = wait.ExponentialBackoffWithContext(ctx, DefaultBackoff, func(context.Context) (done bool, err error) {
		err = s.dialAndServe(ctx, log, addr)
		if err != nil {
			lastErr = err
			if errors.Is(err, context.Canceled) {
				return false, nil
			}

			// we log out the error under debug as this function will be repeated
			// and hopefully will eventually succeed
			// if not then the last observed error should be returned and logged
			// at a higher log level
			log.Debug("Error while attempting to dial and register", "error", err)

			return false, nil
		}

		return true, nil
	})

	// this signifies that the exponential backoff was exhausted or exceeded a deadline
	// in this situation we simply return the last observed error in the dial and serve attempts
	if !errors.Is(err, context.Canceled) && wait.Interrupted(err) {
		err = lastErr
	}

	return err
}

func (s *Tunnel) dialAndServe(
	ctx context.Context,
	log *slog.Logger,
	addr string,
) error {
	tlsConf, err := s.getTLSConfig(addr)
	if err != nil {
		return err
	}

	conn, err := quic.DialAddr(ctx,
		addr,
		tlsConf,
		coallesce(s.QuicConfig, DefaultQuicConfig),
	)
	if err != nil {
		return fmt.Errorf("QUIC dial error: %w", err)
	}

	go func() {
		<-ctx.Done()

		_ = conn.CloseWithError(protocol.ApplicationOK, "")
	}()

	log.Debug("Attempting to register")

	// register server as a listener on remote tunnel
	if err := s.register(conn); err != nil {
		return err
	}

	log.Info("Starting server")

	return (&http3.Server{Handler: s.Handler}).ServeQUICConn(conn)
}

func (s *Tunnel) register(conn quic.Connection) error {
	stream, err := conn.OpenStream()
	if err != nil {
		return fmt.Errorf("accepting stream: %w", err)
	}

	defer stream.Close()

	enc := protocol.NewEncoder[protocol.RegisterListenerRequest](stream)
	defer enc.Close()

	req := &protocol.RegisterListenerRequest{
		Version: protocol.Version,
		Service: s.Namespace,
	}

	auth := defaultAuthenticator
	if s.Authenticator != nil {
		auth = s.Authenticator
	}

	if err := auth.Authenticate(stream.Context(), req); err != nil {
		return fmt.Errorf("registering new connection: %w", err)
	}

	if err := enc.Encode(req); err != nil {
		return fmt.Errorf("encoding register listener request: %w", err)
	}

	dec := protocol.NewDecoder[protocol.RegisterListenerResponse](stream)
	defer dec.Close()

	resp, err := dec.Decode()
	if err != nil {
		return fmt.Errorf("decoding register listener response: %w", err)
	}

	if resp.Code != protocol.CodeOK {
		return fmt.Errorf("unexpected response code: %v", resp.Code)
	}

	if s.OnConnectionReady != nil {
		s.OnConnectionReady(resp)
	}

	return nil
}
