package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nuts-project/nuts/internal/api"
	"github.com/nuts-project/nuts/internal/service"
)

func main() {
	// Initialize service
	log.Println("Starting Nuts Service...")

	// Create core service
	coreService := service.NewService()

	// Start core service (NRI data source)
	if err := coreService.Start(); err != nil {
		log.Fatalf("Failed to start core service: %v", err)
	}
	defer coreService.Stop()

	// Create Gin router
	router := gin.Default()

	// Setup routes
	setupRoutes(router, coreService)

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("HTTP server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}

func setupRoutes(router *gin.Engine, coreService *service.Service) {
	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// Create API handler
	apiHandler := api.NewHandler(coreService.GetPolicyEngine())
	apiHandler.RegisterRoutes(router)
}
