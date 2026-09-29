// Package gateway поднимает HTTP-шлюз (grpc-gateway), который транслирует
// внешние HTTP/JSON запросы в gRPC-вызовы к сервису котировок. Это единая
// точка для сквозной логики (аутентификация, rate limiting, логирование),
// которая не должна затрагивать бизнес-логику самого сервиса.
package gateway

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	quotesv1 "currency-quotes/internal/genpb/quotes/v1"
)

func NewHandler(ctx context.Context, backendAddr string) (http.Handler, error) {
	mux := runtime.NewServeMux()

	conn, err := grpc.NewClient(backendAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	if err := quotesv1.RegisterQuotesServiceHandler(ctx, mux, conn); err != nil {
		return nil, err
	}

	return withMiddlewares(mux), nil
}

// withMiddlewares — точка расширения для сквозной логики. Например,
// авторизация подключается здесь одной строкой, без изменений в сервисе.
func withMiddlewares(h http.Handler) http.Handler {
	h = loggingMiddleware(h)
	// h = authMiddleware(h) // TODO: включить, когда будет готова авторизация
	return h
}
