package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/uvalib/virgo4-jwt/v4jwt"
)

func (svc *serviceContext) virgoJWTMiddleware(c *gin.Context) {
	log.Printf("INFO: authorize user jwt access to %s", c.Request.URL)
	tokenStr, err := getBearerToken(c.Request.Header.Get("Authorization"))
	if err != nil {
		log.Printf("INFO: user jwt auth failed: [%s]", err.Error())
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	if tokenStr == "undefined" {
		log.Printf("INFO: user jwt auth failed; bearer token is undefined")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	log.Printf("INFO: validate jwt auth token...")
	v4Claims, jwtErr := v4jwt.Validate(tokenStr, svc.Secrets.VirgoJWTKey)
	if jwtErr != nil {
		log.Printf("ERROR: jwt signature for %s is invalid: %s", tokenStr, jwtErr.Error())
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// add the parsed claims and signed JWT string to the request context so other handlers can access it.
	c.Set("jwt", tokenStr)
	c.Set("claims", v4Claims)
	log.Printf("INFO: got bearer token: [%s]: %+v", tokenStr, v4Claims)
	c.Next()
}

func getBearerToken(authorization string) (string, error) {
	components := strings.Split(strings.Join(strings.Fields(authorization), " "), " ")

	// must have two components, the first of which is "Bearer", and the second a non-empty token
	if len(components) != 2 || components[0] != "Bearer" || components[1] == "" {
		return "", fmt.Errorf("invalid authorization header: [%s]", authorization)
	}

	return components[1], nil
}

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
func (svc *serviceContext) locationsMiddleware(c *gin.Context) {
	if time.Now().After(svc.Locations.RefreshAt) {
		svc.refreshLocations()
	}
	c.Next()
}
