package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// Version of the service
const version = "0.0.1"

func main() {
	log.Printf("===> ILS Connector service staring up <===")

	// Get config params and use them to init service context. Any issues are fatal
	cfg := loadConfiguration()
	svc, err := intializeService(version, cfg)
	if err != nil {
		log.Fatal(err.Error())
	}

	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	router := gin.Default()
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	corsCfg := cors.DefaultConfig()
	corsCfg.AllowAllOrigins = true
	corsCfg.AllowCredentials = true
	corsCfg.AddAllowHeaders("Authorization")
	router.Use(cors.New(corsCfg))

	router.GET("/", svc.getVersion)
	router.GET("/favicon.ico", svc.ignoreFavicon)
	router.GET("/version", svc.getVersion)
	router.GET("/healthcheck", svc.healthCheck)

	// account management
	router.POST("/users/check_password", svc.sirsiAuthMiddleware, svc.checkPassword)
	router.POST("/users/change_password", svc.sirsiAuthMiddleware, svc.changePassword)
	router.POST("/users/change_password_with_token", svc.sirsiAuthMiddleware, svc.changePasswordWithToken)
	router.POST("/users/:compute_id/forgot_password", svc.sirsiAuthMiddleware, svc.forgotPassword)

	// user data
	router.GET("/users/:compute_id", svc.sirsiAuthMiddleware, svc.getUserInfo)
	router.GET("/users/:compute_id/bills", svc.sirsiAuthMiddleware, svc.getUserBills)
	router.GET("/users/:compute_id/checkouts", svc.sirsiAuthMiddleware, svc.locationsMiddleware, svc.getUserCheckouts)
	router.GET("/users/:compute_id/checkouts.csv", svc.sirsiAuthMiddleware, svc.locationsMiddleware, svc.getUserCheckoutsCSV)
	router.GET("/users/:compute_id/holds", svc.sirsiAuthMiddleware, svc.getUserHolds)

	portStr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Start service v%s on port %s", version, portStr)
	log.Fatal(router.Run(portStr))
}
