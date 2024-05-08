package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Client struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex"`
	Website     string
	Logo        string
	RedirectURI string
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type AuthRequest struct {
	ResponseType string `json:"response_type" query:"response_type"`
	ClientID     string `json:"client_id" query:"client_id"`
	RedirectURI  string `json:"redirect_uri" query:"redirect_uri"`
	Scope        string
	State        string
}

func main() {
	err := godotenv.Load()
	if err != nil {
		panic("Unable to load env file")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		panic("DATABASE_URL is not set!")
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		panic("Failed to connect database")
	}

	// Migrate to schema
	db.AutoMigrate(&Client{})

	// Insert dummy client
	db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "website", "redirect_uri", "logo"}),
	})
	db.Create(&Client{
		ID:          "1",
		Name:        "Fiber",
		Website:     "https://test.com",
		RedirectURI: "http://localhost:8080/callback",
		Logo:        "https://placehold.co/600x400",
	})

	api := fiber.New(fiber.Config{
		AppName: "authorization service",
	})
	api.Use(logger.New())
	api.Use(recover.New())

	api.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello!")
	})

	api.Get("/auth", func(c *fiber.Ctx) error {
		authRequest := new(AuthRequest)
		if err := c.QueryParser(authRequest); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if authRequest.ResponseType != "code" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if authRequest.ClientID != "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if strings.Contains(authRequest.RedirectURI, "https") == "" {
			return c.Status(400).Json(fiber.Map{"error": "invalid_request"})
		}

		return c.SendString("auth!")
	})



	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	api.Listen(fmt.Sprintf(":%s", port))
}
