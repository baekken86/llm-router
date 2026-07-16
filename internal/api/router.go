package api

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

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
	statsHandler *handlers.StatsHandler,
	oauthHandler *handlers.OAuthHandler,
	metadataHandler *handlers.MetadataHandler,
	keyService service.KeyService,
	adminService service.AdminService,
	adminHandler *handlers.AdminHandler,
	webFS *embed.FS,
) chi.Router {
	r := chi.NewRouter()

	r.Use(chimw.Recoverer)
	r.Use(middleware.LoggingMiddleware(logger))

	// Admin auth endpoint (no auth required)
	r.Route("/api/v1/admin", func(r chi.Router) {
		r.Mount("/", adminHandler.Routes())
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(keyService, adminService))

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

		r.Route("/stats", func(r chi.Router) {
			r.Mount("/", statsHandler.Routes())
		})

		r.Route("/metadata", func(r chi.Router) {
			r.Get("/fields", metadataHandler.GetFields)
		})
	})

	r.Route("/v1/oauth", func(r chi.Router) {
		r.Mount("/", oauthHandler.Routes())
	})

	r.Post("/v1/oauth/authorize", oauthHandler.AuthorizePost)

	if webFS != nil {
		serveSPA(r, webFS)
	}

	return r
}

func serveSPA(r chi.Router, webFS *embed.FS) {
	distFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" {
			http.NotFound(w, req)
			return
		}

		path := req.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") {
			http.NotFound(w, req)
			return
		}

		f, err := distFS.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			req.URL.Path = "/"
			fileServer.ServeHTTP(w, req)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, req)
	})
}
