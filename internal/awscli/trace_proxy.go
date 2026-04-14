package awscli

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// startTraceProxy starts a local reverse proxy that forwards to target and injects
// the W3C trace context headers (traceparent, tracestate) from ctx into every request.
// This connects lstk's active OTel span to LocalStack's server-side spans without
// requiring any changes to the AWS CLI or LocalStack.
//
// Returns the proxy URL (e.g. "http://127.0.0.1:<port>") and a stop function.
// If ctx carries no active span, headers are still injected (they will be no-ops on
// the receiving end), so the caller need not special-case the no-span path.
func startTraceProxy(ctx context.Context, target string) (string, func()) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	targetURL, err := url.Parse(target)
	if err != nil || len(carrier) == 0 {
		// No span context to propagate — skip the proxy overhead entirely.
		return target, func() {}
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(targetURL)
			req.Out.Host = targetURL.Host
			for k, v := range carrier {
				req.Out.Header.Set(k, v)
			}
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return target, func() {}
	}

	srv := &http.Server{Handler: proxy}
	go func() { _ = srv.Serve(ln) }()

	return "http://" + ln.Addr().String(), func() { _ = srv.Close() }
}
