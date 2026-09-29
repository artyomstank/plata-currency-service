// Package gateway exposes the public HTTP/JSON API and delegates business
// operations to the internal gRPC service.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	quotesv1 "currency-quotes/internal/genpb/quotes/v1"
)

type Handler struct {
	HTTP http.Handler
	conn *grpc.ClientConn
}

func NewHandler(ctx context.Context, backendAddr string, backendTimeout time.Duration, log *slog.Logger) (*Handler, error) {
	conn, err := grpc.NewClient(
		backendAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(defaultTimeoutInterceptor(backendTimeout)),
		grpc.WithDefaultServiceConfig(readRetryPolicy),
	)
	if err != nil {
		return nil, err
	}

	gatewayMux := runtime.NewServeMux(
		runtime.WithErrorHandler(errorHandler),
		runtime.WithIncomingHeaderMatcher(incomingHeaderMatcher),
		runtime.WithForwardResponseOption(httpStatusModifier),
	)
	if err := quotesv1.RegisterQuotesServiceHandler(ctx, gatewayMux, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	healthClient := grpc_health_v1.NewHealthClient(conn)
	root := http.NewServeMux()
	root.Handle("/v1/", gatewayMux)
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), backendTimeout)
		defer cancel()
		response, err := healthClient.Check(readyCtx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil || response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	return &Handler{HTTP: withMiddlewares(log, root), conn: conn}, nil
}

func (h *Handler) Close() error {
	return h.conn.Close()
}

const readRetryPolicy = `{
  "methodConfig": [{
    "name": [
      {"service": "quotes.v1.QuotesService", "method": "GetQuoteUpdate"},
      {"service": "quotes.v1.QuotesService", "method": "GetLatestQuote"}
    ],
    "waitForReady": true,
    "retryPolicy": {
      "MaxAttempts": 3,
      "InitialBackoff": "0.1s",
      "MaxBackoff": "1s",
      "BackoffMultiplier": 2,
      "RetryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

func defaultTimeoutInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if _, ok := ctx.Deadline(); ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return invoker(callCtx, method, req, reply, cc, opts...)
	}
}

func incomingHeaderMatcher(key string) (string, bool) {
	if strings.EqualFold(key, "Idempotency-Key") || strings.EqualFold(key, "X-Request-ID") {
		return strings.ToLower(key), true
	}
	return runtime.DefaultHeaderMatcher(key)
}

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func errorHandler(
	ctx context.Context,
	_ *runtime.ServeMux,
	_ runtime.Marshaler,
	w http.ResponseWriter,
	_ *http.Request,
	err error,
) {
	code := status.Code(err)
	message := status.Convert(err).Message()
	if code == codes.Unknown {
		code = codes.Internal
		message = "internal error"
	}
	writeJSON(w, runtime.HTTPStatusFromCode(code), errorResponse{
		Code:      publicErrorCode(code),
		Message:   message,
		RequestID: requestIDFromContext(ctx),
	})
}

func publicErrorCode(code codes.Code) string {
	switch code {
	case codes.InvalidArgument:
		return "INVALID_ARGUMENT"
	case codes.NotFound:
		return "NOT_FOUND"
	case codes.AlreadyExists:
		return "IDEMPOTENCY_CONFLICT"
	case codes.DeadlineExceeded:
		return "GATEWAY_TIMEOUT"
	case codes.Unavailable:
		return "SERVICE_UNAVAILABLE"
	default:
		return "INTERNAL_ERROR"
	}
}

func httpStatusModifier(ctx context.Context, w http.ResponseWriter, _ proto.Message) error {
	metadata, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return nil
	}
	values := metadata.HeaderMD.Get("x-http-code")
	if len(values) == 0 {
		return nil
	}
	statusCode, err := strconv.Atoi(values[0])
	if err != nil {
		return err
	}
	delete(metadata.HeaderMD, "x-http-code")
	w.WriteHeader(statusCode)
	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
