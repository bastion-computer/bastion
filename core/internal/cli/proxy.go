package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

type proxyRunner func(context.Context, io.Writer, proxyOptions) error

type proxyOptions struct {
	apiURL         string
	namespaceID    string
	namespaceKey   string
	environmentID  string
	environmentKey string
	name           string
	port           int
}

func newProxyCommand(opts *rootOptions) *cobra.Command {
	return newProxyCommandWithRunner(opts, runProxy)
}

func newProxyCommandWithRunner(opts *rootOptions, runner proxyRunner) *cobra.Command {
	if runner == nil {
		runner = runProxy
	}

	proxyOpts := proxyOptions{}

	cmd := &cobra.Command{
		Use:   proxyUse + " (--env-id ID | --env-key KEY) --name NAME [--port PORT]",
		Short: "Start a local proxy for an environment tunnel",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireProxyEnvironmentReference(proxyOpts.environmentID, proxyOpts.environmentKey); err != nil {
				return err
			}

			if proxyOpts.name == "" {
				return errors.New("--name is required")
			}

			if proxyOpts.port < 0 || proxyOpts.port > 65535 {
				return errors.New("--port must be between 0 and 65535")
			}

			proxyOpts.apiURL = opts.apiURL
			proxyOpts.namespaceID = opts.namespaceID
			proxyOpts.namespaceKey = opts.namespaceKey

			return runner(cmd.Context(), cmd.ErrOrStderr(), proxyOpts)
		},
	}
	cmd.Flags().StringVar(&proxyOpts.environmentID, "env-id", "", "environment ID")
	cmd.Flags().StringVar(&proxyOpts.environmentKey, "env-key", "", "environment key")
	cmd.Flags().StringVar(&proxyOpts.name, "name", "", "environment tunnel name")
	cmd.Flags().IntVar(&proxyOpts.port, "port", 0, "local proxy port (0 selects a free port)")

	return cmd
}

func requireProxyEnvironmentReference(id, key string) error {
	if (id == "") == (key == "") {
		return errors.New("specify exactly one of --env-id or --env-key")
	}

	return nil
}

func runProxy(ctx context.Context, stderr io.Writer, opts proxyOptions) error {
	if err := validateProxyTunnel(ctx, opts); err != nil {
		return err
	}

	target, err := parseProxyTarget(environmentTunnelURL(opts.apiURL, opts.environmentID, opts.environmentKey, opts.name, opts.namespaceID, opts.namespaceKey))
	if err != nil {
		return fmt.Errorf("parse tunnel URL: %w", err)
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.port)))
	if err != nil {
		return fmt.Errorf("listen local proxy: %w", err)
	}

	defer func() { _ = listener.Close() }()

	localURL := "http://" + listener.Addr().String()
	_, _ = fmt.Fprintf(stderr, "proxy listening on %s\n", localURL)
	_, _ = fmt.Fprintf(stderr, "proxy target %s\n", target.String())

	server := &http.Server{
		Handler:           newProxyHandler(target, stderr),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)

	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}

		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown local proxy: %w", err)
		}

		return <-errCh
	}
}

func validateProxyTunnel(ctx context.Context, opts proxyOptions) error {
	tunnels, err := apiClient(&rootOptions{apiURL: opts.apiURL, namespaceID: opts.namespaceID, namespaceKey: opts.namespaceKey}).GetEnvironmentTunnels(ctx, opts.environmentID, opts.environmentKey)
	if err != nil {
		return err
	}

	for _, tunnel := range tunnels.Entries {
		if tunnel.Name == opts.name {
			return nil
		}
	}

	return fmt.Errorf("environment tunnel %q not found", opts.name)
}

func parseProxyTarget(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, err
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("tunnel URL must include a scheme and host")
	}

	parsed.RawPath = escapedPathFromAbsoluteURL(value, parsed)

	return parsed, nil
}

func escapedPathFromAbsoluteURL(value string, parsed *url.URL) string {
	authority := parsed.Host
	if parsed.User != nil {
		authority = parsed.User.String() + "@" + authority
	}

	prefix := parsed.Scheme + "://" + authority

	rest := strings.TrimPrefix(value, prefix)
	if !strings.HasPrefix(rest, "/") {
		return ""
	}

	if index := strings.IndexAny(rest, "?#"); index >= 0 {
		return rest[:index]
	}

	return rest
}

func newProxyHandler(target *url.URL, logs io.Writer) http.Handler {
	logger := &proxyRequestLogger{w: logs}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			rewriteProxyRequest(req, target)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		proxyWriter := &proxyResponseWriter{ResponseWriter: w, status: http.StatusOK}

		proxy.ServeHTTP(proxyWriter, r)
		logger.log(r, proxyWriter.status, time.Since(start))
	})
}

func rewriteProxyRequest(req *http.Request, target *url.URL) {
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path = joinProxyPath(target.Path, req.URL.Path)
	req.URL.RawPath = joinProxyPath(proxyTargetEscapedPath(target), req.URL.EscapedPath())
	req.URL.RawQuery = joinProxyQuery(target.RawQuery, req.URL.RawQuery)
	req.Host = target.Host
}

func joinProxyQuery(base, request string) string {
	switch {
	case base == "":
		return request
	case request == "":
		return base
	default:
		return base + "&" + request
	}
}

func proxyTargetEscapedPath(target *url.URL) string {
	if target.RawPath != "" {
		return target.RawPath
	}

	return target.EscapedPath()
}

func joinProxyPath(base, path string) string {
	if base == "" || base == "/" {
		if path == "" {
			return "/"
		}

		return path
	}

	base = strings.TrimRight(base, "/")
	if path == "" || path == "/" {
		return base + "/"
	}

	return base + "/" + strings.TrimLeft(path, "/")
}

type proxyRequestLogger struct {
	w  io.Writer
	mu sync.Mutex
}

func (l *proxyRequestLogger) log(r *http.Request, status int, duration time.Duration) {
	if l.w == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	_, _ = fmt.Fprintf(l.w, "%s %s -> %d %s\n", r.Method, r.URL.RequestURI(), status, duration.Round(time.Millisecond))
}

type proxyResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *proxyResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *proxyResponseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *proxyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.status = http.StatusSwitchingProtocols

	return http.NewResponseController(w.ResponseWriter).Hijack()
}
