package main

import (
	"context"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	mongoopts "go.mongodb.org/mongo-driver/mongo/options"

	"github.com/AFDEAPAC/swallow/bootstrap"
	"github.com/AFDEAPAC/swallow/config"
	authdelivery "github.com/AFDEAPAC/swallow/internal/auth/delivery"
	authapp "github.com/AFDEAPAC/swallow/internal/auth/application"
	authinfra "github.com/AFDEAPAC/swallow/internal/auth/infra"
	serverdelivery "github.com/AFDEAPAC/swallow/internal/server/delivery"
	serverapp "github.com/AFDEAPAC/swallow/internal/server/application"
	serverinfra "github.com/AFDEAPAC/swallow/internal/server/infra"
	"github.com/AFDEAPAC/swallow/internal/shared/jwt"
	"github.com/AFDEAPAC/swallow/internal/shared/middleware"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("mongo ping: %v", err)
	}
	log.Println("connected to mongodb")

	db := client.Database(cfg.MongoDB)

	userRepo := authinfra.NewMongoUserRepo(db)
	serverRepo, err := serverinfra.NewMongoServerRepo(db)
	if err != nil {
		log.Fatalf("server repo init: %v", err)
	}

	if err := bootstrap.EnsureAdminUser(context.Background(), userRepo, cfg.AdminUser, cfg.AdminPass); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	jwtSvc := jwt.NewService(cfg.JWTSecret, cfg.JWTExpiry)

	loginUC := authapp.NewLoginUseCase(userRepo, jwtSvc)
	meUC := authapp.NewMeUseCase(userRepo)
	authHandler := authdelivery.NewAuthHandler(loginUC, meUC)

	createServerUC := serverapp.NewCreateServerUseCase(serverRepo)
	listServersUC := serverapp.NewListServersUseCase(serverRepo)
	deleteServerUC := serverapp.NewDeleteServerUseCase(serverRepo)
	serverHandler := serverdelivery.NewServerHandler(createServerUC, listServersUC, deleteServerUC)

	app := fiber.New(fiber.Config{
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
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,DELETE,OPTIONS",
		AllowHeaders: "Content-Type,Authorization",
	}))

	v1 := app.Group("/api/v1")

	auth := v1.Group("/auth")
	auth.Post("/login", authHandler.Login)
	auth.Get("/me", middleware.Auth(jwtSvc), authHandler.Me)

	servers := v1.Group("/servers", middleware.Auth(jwtSvc), middleware.AdminOnly())
	servers.Post("/", serverHandler.Create)
	servers.Get("/", serverHandler.List)
	servers.Delete("/:id", serverHandler.Delete)

	log.Printf("starting server on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
