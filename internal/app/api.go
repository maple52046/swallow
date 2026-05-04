// Package app provides top-level runtime bootstrap functions.
// Each function accepts a fully assembled config object so that no application
// code needs to parse environment variables or CLI flags directly.
package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"go.mongodb.org/mongo-driver/mongo"
	mongoopts "go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"

	"github.com/AFDEAPAC/swallow/bootstrap"
	"github.com/AFDEAPAC/swallow/config"
	agentv1 "github.com/AFDEAPAC/swallow/gen/agent/v1"
	authapp "github.com/AFDEAPAC/swallow/internal/auth/application"
	authdelivery "github.com/AFDEAPAC/swallow/internal/auth/delivery"
	authinfra "github.com/AFDEAPAC/swallow/internal/auth/infra"
	serverapp "github.com/AFDEAPAC/swallow/internal/server/application"
	serverdelivery "github.com/AFDEAPAC/swallow/internal/server/delivery"
	serverinfra "github.com/AFDEAPAC/swallow/internal/server/infra"
	"github.com/AFDEAPAC/swallow/internal/shared/jwt"
	"github.com/AFDEAPAC/swallow/internal/shared/middleware"
)

// RunAPI starts the HTTP API server and the gRPC agent service using the provided config.
// It connects to MongoDB, bootstraps the admin user, registers all routes,
// and blocks until the HTTP server exits.
func RunAPI(cfg config.APIConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}
	log.Println("connected to mongodb")

	db := client.Database(cfg.MongoDB)

	userRepo := authinfra.NewMongoUserRepo(db)
	serverRepo, err := serverinfra.NewMongoServerRepo(db)
	if err != nil {
		return fmt.Errorf("server repo init: %w", err)
	}

	if err := bootstrap.EnsureAdminUser(
		context.Background(), userRepo,
		cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword,
	); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	// JWTExpiryHours is stored as int; convert here so domain code stays decoupled.
	jwtSvc := jwt.NewService(cfg.JWTSecret, time.Duration(cfg.JWTExpiryHours)*time.Hour)

	loginUC := authapp.NewLoginUseCase(userRepo, jwtSvc)
	meUC := authapp.NewMeUseCase(userRepo)
	authHandler := authdelivery.NewAuthHandler(loginUC, meUC)

	createServerUC := serverapp.NewCreateServerUseCase(serverRepo)
	listServersUC := serverapp.NewListServersUseCase(serverRepo)
	deleteServerUC := serverapp.NewDeleteServerUseCase(serverRepo)
	serverHandler := serverdelivery.NewServerHandler(createServerUC, listServersUC, deleteServerUC)

	// gRPC agent service — only started when nodeAuthToken is configured.
	// When nodeAuthToken is absent the HTTP/JWT service continues to work normally;
	// agents simply cannot connect until the token is configured and the server restarted.
	if cfg.NodeAuthToken != "" {
		updateInventoryUC := serverapp.NewUpdateInventoryUseCase(serverRepo)
		agentGRPCHandler := serverdelivery.NewAgentGRPCHandler(updateInventoryUC, serverRepo, cfg.NodeAuthToken)
		if err := startGRPCServer(cfg.GRPCAddr, agentGRPCHandler); err != nil {
			return fmt.Errorf("grpc server: %w", err)
		}
	} else {
		log.Println("nodeAuthToken not configured; gRPC agent service is disabled")
	}

	fiberApp := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Printf("unhandled error: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "internal_error",
					"message": "An unexpected error occurred.",
				},
			})
		},
	})

	// Allow cross-origin requests so browser preflight OPTIONS is handled before
	// any route matching occurs. AllowOrigins "*" is appropriate for an internal
	// management platform; restrict to a specific origin if needed.
	fiberApp.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,DELETE,OPTIONS",
		AllowHeaders: "Content-Type,Authorization",
	}))

	v1 := fiberApp.Group("/api/v1")

	auth := v1.Group("/auth")
	auth.Post("/login", authHandler.Login)
	auth.Get("/me", middleware.Auth(jwtSvc), authHandler.Me)

	servers := v1.Group("/servers", middleware.Auth(jwtSvc), middleware.AdminOnly())
	servers.Post("/", serverHandler.Create)
	servers.Get("/", serverHandler.List)
	servers.Delete("/:id", serverHandler.Delete)

	log.Printf("starting HTTP server on %s", cfg.Addr)
	return fiberApp.Listen(cfg.Addr)
}

// startGRPCServer creates a TCP listener and starts the gRPC server in a goroutine.
// The server runs for the lifetime of the process; errors after startup are logged.
func startGRPCServer(addr string, handler *serverdelivery.AgentGRPCHandler) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	grpcSrv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(grpcSrv, handler)

	log.Printf("starting gRPC agent service on %s", addr)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()
	return nil
}
