package api

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/chris/llm-router/internal/api/handlers"
	"github.com/chris/llm-router/internal/api/middleware"
	"github.com/chris/llm-router/internal/service"
)

func NewRouter(
	logger *slog.Logger,
	providerHandler *handlers.ProviderHandler,
	modelHandler *handlers.ModelHandler,
	vmHandler *handlers.VirtualModelHandler,
	keyHandler *handlers.KeyHandler,
	importHandler *handlers.ImportHandler,
	keyService service.KeyService,
) chi.Router {
	r := chi.NewRouter()

	r.Use(chimw.Recoverer)
	r.Use(middleware.LoggingMiddleware(logger))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(keyService))

		r.Route("/providers", func(r chi.Router) {
			r.Mount("/", providerHandler.Routes())
		})

		r.Route("/models", func(r chi.Router) {
			r.Mount("/", modelHandler.Routes())
		})

		r.Route("/virtual-models", func(r chi.Router) {
			r.Mount("/", vmHandler.Routes())
		})

		r.Route("/keys", func(r chi.Router) {
			r.Mount("/", keyHandler.Routes())
		})

		r.Route("/import", func(r chi.Router) {
			r.Mount("/", importHandler.Routes())
		})
	})

	return r
}
