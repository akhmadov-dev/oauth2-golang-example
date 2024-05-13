package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"github.com/gofiber/template/html/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lucsky/cuid"
	"os"
	"strconv"
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
	ID           string `gorm:"primaryKey"`
	Name         string `gorm:"uniqueIndex"`
	ClientSecret string `json:"-"`
	Website      string
	Logo         string
	Code         sql.NullString `gorm:"default:null"`
	RedirectURI  string
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

type AuthRequest struct {
	ResponseType string `json:"response_type" query:"response_type"`
	ClientID     string `json:"client_id" query:"client_id"`
	RedirectURI  string `json:"redirect_uri" query:"redirect_uri"`
	Scope        string
	State        string
}

type ConfirmAuthRequest struct {
	Authorize bool   `json:"authorize" query:"authorize"`
	ClientID  string `json:"client_id" query:"client_id"`
	State     string `json:"state" query:"state"`
}

type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
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

	// Generate temp code
	clientSecret, err := cuid.NewCrypto(rand.Reader)
	if err != nil {
		panic("Failed to generate client secret")
	}

	// Insert dummy client
	db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "website", "redirect_uri", "logo", "client_secret"}),
	})
	db.Create(&Client{
		ID:           "1",
		Name:         "Fiber",
		Website:      "https://test.com",
		RedirectURI:  "http://localhost:8080/callback",
		Logo:         "https://placehold.co/600x400",
		ClientSecret: clientSecret,
	})

	views := html.New("./views", ".html")
	api := fiber.New(fiber.Config{
		AppName: "Authorization Service",
		Views:   views,
	})
	api.Use(logger.New())
	api.Use(recover.New())

	api.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello!")
	})
	api.Get("/auth", func(c *fiber.Ctx) error {
		// Parse Request
		authRequest := new(AuthRequest)
		if err := c.QueryParser(authRequest); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err})
		}
		//fmt.Println(authRequest)

		if authRequest.ResponseType != "code" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if authRequest.ClientID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if strings.Contains(authRequest.RedirectURI, "https") == false {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if authRequest.Scope == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		if authRequest.State == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_state"})
		}

		// Check for client
		client := new(Client)
		if err := db.Where("name=?", authRequest.ClientID).First(&client).Error; err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_client"})
		}

		// Generate temp code
		code, err := cuid.NewCrypto(rand.Reader)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "server_error"})
		}

		c.Cookie(&fiber.Cookie{
			Name:     "temp_auth_request_code",
			Value:    code,
			Secure:   true,
			Expires:  time.Now().Add(1 * time.Hour),
			HTTPOnly: true,
		})

		return c.Render("authorize_client", fiber.Map{
			"Logo":    client.Logo,
			"Name":    client.Name,
			"Website": client.Website,
			"State":   authRequest.State,
			"Scopes":  strings.Split(authRequest.Scope, " "),
		})
	})
	api.Get("/confirm_auth", func(c *fiber.Ctx) error {
		tempCode := c.Cookies("temp_auth_request_code")
		if tempCode == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		confirmAuthRequest := new(ConfirmAuthRequest)
		if err := c.QueryParser(confirmAuthRequest); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		// Check for client
		client := new(Client)
		if err := db.Where("name=?", confirmAuthRequest.ClientID).First(&client).Error; err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_client"})
		}

		// Redirect based on deny
		if !confirmAuthRequest.Authorize {
			return c.Redirect(client.RedirectURI + "?error=invalid_request" + "&state=" + confirmAuthRequest.State)
		}

		// Save generated auth code to client table
		db.Model(&client).Update("code", tempCode)

		return c.Redirect(client.RedirectURI + "?code=" + tempCode + "&state=" + confirmAuthRequest.State)
	})

	api.Post("/token", func(c *fiber.Ctx) error {
		tokenRequest := new(TokenRequest)
		if err := c.BodyParser(tokenRequest); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		// Validate params
		if tokenRequest.ClientID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}
		if tokenRequest.Code == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}
		if tokenRequest.RedirectURI == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}
		if tokenRequest.ClientSecret == "" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}
		if tokenRequest.GrantType != "authorization_code" {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_request"})
		}

		// Lookup client
		client := new(Client)
		if err := db.Where("name=?", tokenRequest.ClientID).First(&client).Error; err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "client_not_found"})
		}

		// Validate client and code
		if !client.Code.Valid {
			return c.Status(400).JSON(fiber.Map{"error": err})
		}
		if tokenRequest.Code != client.Code.String {
			return c.Status(400).JSON(fiber.Map{"error": "invalid_code1"})
		}

		// Generate the access token
		token := jwt.New(jwt.SigningMethodHS256)
		claims := token.Claims.(jwt.MapClaims)
		claims["exp"] = time.Now().Add(time.Hour * 6).Unix()

		accessToken, err := token.SignedString([]byte(client.ClientSecret))
		if err != nil {
			return c.SendString(strconv.Itoa(fiber.StatusInternalServerError))
		}

		tokenResponse := new(TokenResponse)
		tokenResponse.AccessToken = accessToken
		tokenResponse.ExpiresIn = 3600

		return c.Status(200).JSON(tokenResponse)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	api.Listen(fmt.Sprintf(":%s", port))
}
