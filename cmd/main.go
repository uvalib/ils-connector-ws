package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// Version of the service
const version = "1.5.2"

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

	// availability
	router.GET("/availability/list", svc.sirsiAuthMiddleware, svc.refreshDataMiddleware, svc.getAvailabilityList)
	router.GET("/availability/:cat_key", svc.refreshDataMiddleware, svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.getAvailability)

	// course reserves management
	router.POST("/course_reserves", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.refreshDataMiddleware, svc.createCourseReserves)
	router.POST("/course_reserves/validate", svc.sirsiAuthMiddleware, svc.validateCourseReserves)
	router.GET("/course_reserves/search", svc.sirsiAuthMiddleware, svc.searchCourseReserves)

	// dibs management
	router.PUT("/dibs/indibs/:barcode", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.setBarcodeInDiBS)
	router.PUT("/dibs/nodibs/:barcode", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.setBarcodeNotInDiBS)
	router.POST("/dibs/checkin", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.checkinDiBS)
	router.POST("/dibs/checkout", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.checkoutDiBS)

	// metadata rights update
	router.POST("/metadata/:cat_key/update_rights", svc.sirsiAuthMiddleware, svc.updateMetadataRights)

	// account management
	router.POST("/users/check_password", svc.sirsiAuthMiddleware, svc.checkPassword)
	router.POST("/users/change_password", svc.sirsiAuthMiddleware, svc.changePassword)
	router.POST("/users/change_password_with_token", svc.sirsiAuthMiddleware, svc.changePasswordWithToken)
	router.POST("/users/forgot_password", svc.sirsiAuthMiddleware, svc.forgotPassword)
	router.POST("/users/register", svc.sirsiAuthMiddleware, svc.registerNewUser)
	router.GET("/users/activate/:token", svc.sirsiAuthMiddleware, svc.activateUser)

	// user data
	router.GET("/users/:compute_id", svc.sirsiAuthMiddleware, svc.getUserInfo)
	router.GET("/users/:compute_id/bills", svc.sirsiAuthMiddleware, svc.getUserBills)
	router.GET("/users/:compute_id/checkouts", svc.sirsiAuthMiddleware, svc.refreshDataMiddleware, svc.getUserCheckouts)
	router.GET("/users/:compute_id/checkouts.csv", svc.sirsiAuthMiddleware, svc.refreshDataMiddleware, svc.getUserCheckoutsCSV)
	router.GET("/users/:compute_id/holds", svc.sirsiAuthMiddleware, svc.getUserHolds)

	// hold and scan requests; all but fill_hold is done by a virgo user and requires a virgo jwt
	router.POST("/requests/hold", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.createHold)
	router.DELETE("/requests/hold/:id", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.deleteHold)
	router.POST("/requests/scan", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.createScan)
	router.POST("/requests/renew", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.renewCheckouts)

	// used by leo hold filler (or tedium reducer): barcode scanning
	router.POST("/users/sirsi_staff_login", svc.sirsiAuthMiddleware, svc.staffLogin)
	router.POST("/requests/fill_hold/:barcode", svc.sirsiAuthMiddleware, svc.fillHold)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGTERM,
		syscall.SIGINT,
	)
	go func() {
		s := <-sigc
		log.Printf("INFO: caught %s ", s)
		svc.terminateSession()
		os.Exit(0)
	}()

	portStr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Start service v%s on port %s", version, portStr)
	log.Fatal(router.Run(portStr))
}
