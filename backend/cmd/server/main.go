package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mimi6060/GeoNote/backend/internal/cache"
	"github.com/mimi6060/GeoNote/backend/internal/config"
	"github.com/mimi6060/GeoNote/backend/internal/handler"
	"github.com/mimi6060/GeoNote/backend/internal/middleware"
	"github.com/mimi6060/GeoNote/backend/internal/repository"
	"github.com/mimi6060/GeoNote/backend/internal/service"
	"github.com/mimi6060/GeoNote/backend/internal/ws"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("configuration invalide:\n%v", err)
	}

	// ---- Database (PostgreSQL + PostGIS) ----
	poolCfg, err := cfg.PoolConfig()
	if err != nil {
		log.Fatalf("connexion DB impossible: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		log.Fatalf("connexion DB impossible: %v", err)
	}
	defer pool.Close()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 15*time.Second)
	err = pool.Ping(pingCtx)
	cancelPing()
	if err != nil {
		log.Fatalf("ping DB echoue: %v", err)
	}
	log.Printf("connecte a PostgreSQL + PostGIS (pool max %d)", cfg.DBMaxConns)

	// ---- Redis Cache ----
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		log.Printf("[cache] %v, Redis desactive", err)
	}
	redisCache := cache.NewRedisCache(redisOpts, cfg.CacheTTL)
	defer redisCache.Close()

	// ---- Limiteurs (partages via Redis entre instances, sinon en memoire) ----
	ipLimiter, postLimiter := newLimiters(redisCache.Client())

	// ---- WebSocket Hub ----
	hub := ws.NewHub()

	// ---- Repositories ----
	userRepo := repository.NewUserRepo(pool)
	messageRepo := repository.NewMessageRepo(pool)
	interactionRepo := repository.NewInteractionRepo(pool)
	gamRepo := repository.NewGamificationRepo(pool)

	// ---- Services ----
	authSvc := service.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTExpiry)
	authSvc.OnAccountDeleted(redisCache.InvalidateZone)
	messageSvc := service.NewMessageService(messageRepo, gamRepo, interactionRepo, redisCache, hub)
	interactionSvc := service.NewInteractionService(interactionRepo)
	gamSvc := service.NewGamificationService(gamRepo)
	searchSvc := service.NewSearchService(messageRepo, userRepo)

	// ---- Handlers ----
	authH := handler.NewAuthHandler(authSvc)
	messageH := handler.NewMessageHandler(messageSvc)
	interactionH := handler.NewInteractionHandler(interactionSvc)
	gamH := handler.NewGamificationHandler(gamSvc)
	searchH := handler.NewSearchHandler(searchSvc)

	// ---- Router ----
	r := chi.NewRouter()

	// Middlewares globaux
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))
	r.Use(middleware.TrustedRealIP(cfg.TrustedIPHeader))
	r.Use(middleware.CORS(cfg.CORSAllowedOrigins))
	r.Use(middleware.RequestID)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RateLimit(ipLimiter)) // 10 req/s, burst 20, par IP

	// ---- Routes ----
	r.Route("/api/v1", func(r chi.Router) {
		// Health
		r.Get("/health", handler.HealthCheck(pool))

		// Auth (public)
		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)

		// Public routes with optional auth
		r.Group(func(r chi.Router) {
			r.Use(middleware.OptionalAuth(authSvc))

			r.Get("/messages/nearby", messageH.GetNearby)
			r.Get("/heatmap", messageH.GetHeatmap)
			r.Get("/leaderboard", messageH.GetLeaderboard)
			r.Get("/events", messageH.DetectEvents)
		})

		// Search (public)
		r.Get("/search", searchH.Search)
		r.Get("/search/hashtags/popular", searchH.PopularHashtags)
		r.Get("/users/{id}/messages", messageH.GetByUser)
		r.Get("/users/{id}/profile", gamH.GetUserProfile)

		// Authenticated routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(authSvc))

			r.Get("/auth/me", authH.Me)
			r.Get("/me/profile", gamH.GetMyProfile)
			r.Delete("/me", authH.DeleteAccount)

			r.With(middleware.AntiSpam(postLimiter)).Post("/messages", messageH.Create)
			r.Delete("/messages/{id}", messageH.Delete)
			r.Post("/messages/{id}/unlock", messageH.UnlockMystery)

			r.Post("/messages/{id}/like", interactionH.ToggleLike)
			r.Post("/messages/{id}/react", interactionH.ToggleReaction)
			r.Post("/messages/{id}/comments", interactionH.AddComment)
			r.Post("/messages/{id}/report", interactionH.ReportMessage)
			r.Delete("/comments/{commentId}", interactionH.DeleteComment)
		})

		// Comments (public read)
		r.Get("/messages/{id}/comments", interactionH.GetComments)
	})

	// WebSocket (with optional token auth)
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		var userID string
		token := r.URL.Query().Get("token")
		if token != "" {
			if uid, err := authSvc.ValidateToken(token); err == nil {
				userID = uid
			}
		}
		ws.ServeWS(hub, w, r, userID)
	})

	// ---- Server ----
	// PORT est fourni par Vercel (preset Go) ; 8080 en local et dans Docker.
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("GeoNote API sur http://localhost:%s", cfg.Port)
		log.Printf("WebSocket sur ws://localhost:%s/ws", cfg.Port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("erreur serveur: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("arret en cours...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("arret force: %v", err)
	}
	log.Println("serveur arrete")
}
