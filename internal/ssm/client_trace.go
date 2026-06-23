package ssm

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/logging"
)

func traceHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{Transport: traceRoundTripper{base: transport}}
}

type traceRoundTripper struct {
	base http.RoundTripper
}

func (transport traceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}

	ctx := req.Context()
	logger := logging.FromContext(ctx)
	started := time.Now()
	host := req.URL.Host
	attrs := []slog.Attr{
		slog.String("method", req.Method),
		slog.String("host", host),
		slog.String("url", req.URL.String()),
	}

	var dnsStarted, connectStarted, tlsStarted, wroteRequestAt time.Time

	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStarted = time.Now()

			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "dns_start"), slog.String("dns_host", info.Host))...)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "dns_done"), slog.Int64("duration_ms", elapsedMillis(dnsStarted)), slog.Any("addrs", info.Addrs), slog.Any("error", info.Err))...)
		},
		ConnectStart: func(network, addr string) {
			connectStarted = time.Now()

			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "connect_start"), slog.String("network", network), slog.String("addr", addr))...)
		},
		ConnectDone: func(network, addr string, err error) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "connect_done"), slog.Int64("duration_ms", elapsedMillis(connectStarted)), slog.String("network", network), slog.String("addr", addr), slog.Any("error", err))...)
		},
		TLSHandshakeStart: func() {
			tlsStarted = time.Now()

			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "tls_start"))...)
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			_ = state

			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "tls_done"), slog.Int64("duration_ms", elapsedMillis(tlsStarted)), slog.Any("error", err))...)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "got_conn"), slog.Bool("reused", info.Reused), slog.Bool("was_idle", info.WasIdle), slog.Int64("idle_ms", int64(info.IdleTime/time.Millisecond)))...)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			wroteRequestAt = time.Now()

			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "wrote_request"), slog.Int64("elapsed_ms", elapsedMillis(started)), slog.Any("error", info.Err))...)
		},
		GotFirstResponseByte: func() {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "first_response_byte"), slog.Int64("elapsed_ms", elapsedMillis(started)), slog.Int64("server_wait_ms", elapsedMillis(wroteRequestAt)))...)
		},
	}

	tracedRequest := req.WithContext(httptrace.WithClientTrace(ctx, trace))
	resp, err := base.RoundTrip(tracedRequest)

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	logger.LogAttrs(ctx, logging.LevelTrace, "aws http request completed", append(attrs, slog.Int("status", statusCode), slog.Int64("duration_ms", elapsedMillis(started)), slog.Any("error", err))...)

	if err != nil {
		return resp, errors.Wrap(err, "aws http request")
	}

	return resp, nil
}

func elapsedMillis(started time.Time) int64 {
	if started.IsZero() {
		return 0
	}

	return int64(time.Since(started) / time.Millisecond)
}
