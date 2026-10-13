package main

import (
	"log"
	"net/http"

	"github.com/cseptian/rukun-warga-api/internal/database"
	"github.com/cseptian/rukun-warga-api/internal/handlers"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize database
	database.InitDB()

	// Setup Gin router
	r := gin.Default()

	// Configure CORS
	r.Use(cors.Default())

	// Health check endpoint for k8s
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "up"})
	})

	// Define API routes
	api := r.Group("/api")
	{
		// Authentication Routes
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", handlers.Login)
			authGroup.GET("/me", handlers.AuthMiddleware(), handlers.GetMe)
		}

		// Water System Routes (protected)
		water := api.Group("/water")
		water.Use(handlers.AuthMiddleware())
		{
			water.GET("/neighbors", handlers.RequirePermission("water:read"), handlers.ListNeighbors)
			water.GET("/neighbors/:id", handlers.RequirePermission("water:read"), handlers.GetNeighbor)
			water.POST("/neighbors", handlers.RequirePermission("water:manage"), handlers.CreateNeighbor)
			water.POST("/readings", handlers.RequirePermission("water:manage"), handlers.CreateReading)
			water.PATCH("/readings/:id/toggle-payment", handlers.RequirePermission("water:manage"), handlers.TogglePaymentStatus)
		}

		// Treasury Routes (protected)
		treasury := api.Group("/treasury")
		treasury.Use(handlers.AuthMiddleware())
		{
			treasury.GET("/summary", handlers.RequirePermission("treasury:read"), handlers.GetTreasurySummary)
			treasury.GET("/my-dues", handlers.RequirePermission("treasury:read"), handlers.GetMyDues)
			treasury.GET("/transactions", handlers.RequirePermission("treasury:write"), handlers.ListTransactions)
			treasury.POST("/transactions", handlers.RequirePermission("treasury:write"), handlers.CreateTransaction)
		}

		// Announcement Routes
		api.GET("/announcements", handlers.ListAnnouncements)
		api.POST("/announcements", handlers.AuthMiddleware(), handlers.RequirePermission("announcements:publish"), handlers.CreateAnnouncement)
	}

	// Start server
	log.Println("Starting Rukun Warga API on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
