package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (svc *serviceContext) sirsiAuthMiddleware(c *gin.Context) {
	log.Printf("INFO: ensure sirsi session exists for %s", c.Request.URL)
	if svc.SirsiSession.SessionToken == "" || svc.SirsiSession.isExpired() {
		err := svc.sirsiLogin()
		if err != nil {
			log.Printf("ERROR: %s", err.Error())
			c.AbortWithError(http.StatusForbidden, err)
			return
		}
	}
	c.Next()
}
