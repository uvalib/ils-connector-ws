package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// Version of the service
const version = "0.9.0"

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
	// TODO move API from v4-availability-ws here

	// course reserves management
	router.POST("/course_reserves/validate", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.validateCourseReserves)
	// TODO move search and other reserves functionality from avail service here

	// dibs management
	router.PUT("/dibs/indibs/:barcode", svc.sirsiAuthMiddleware, svc.setBarcodeInDiBS)
	router.PUT("/dibs/nodibs/:barcode", svc.sirsiAuthMiddleware, svc.setBarcodeNotInDiBS)
	router.POST("/dibs/checkin", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.checkinDiBS)
	router.POST("/dibs/checkout", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.checkoutDiBS)

	// metadata rights update
	router.POST("/metadata/:cat_key/update_rights", svc.sirsiAuthMiddleware, svc.updateMetadataRights)

	// account management
	router.POST("/users/check_password", svc.sirsiAuthMiddleware, svc.checkPassword)
	router.POST("/users/change_password", svc.sirsiAuthMiddleware, svc.changePassword)
	router.POST("/users/change_password_with_token", svc.sirsiAuthMiddleware, svc.changePasswordWithToken)
	router.POST("/users/:compute_id/forgot_password", svc.sirsiAuthMiddleware, svc.forgotPassword)
	router.POST("/users/register", svc.sirsiAuthMiddleware, svc.registerNewUser)
	router.GET("/users/activate/:token", svc.sirsiAuthMiddleware, svc.activateUser)
	router.POST("/users/sirsi_staff_login", svc.sirsiAuthMiddleware, svc.staffLogin)

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
	router.POST("/requests/fill_hold/:barcode", svc.sirsiAuthMiddleware, svc.fillHold)
	router.POST("/requests/renew", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.renewCheckouts)

	// dummy API to map old calls to new for renew. REMOVE WHEN virgo can be updated
	router.POST("/request/renew", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.renewCheckouts)
	router.POST("/request/renewall", svc.sirsiAuthMiddleware, svc.virgoJWTMiddleware, svc.renewCheckouts)

	portStr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Start service v%s on port %s", version, portStr)
	log.Fatal(router.Run(portStr))
}
