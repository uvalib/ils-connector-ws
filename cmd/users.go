package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (svc *serviceContext) getUserInfo(c *gin.Context) {
	computeID := c.Param("compute_id")
	if computeID == "" {
		c.String(http.StatusBadRequest, "compute_id is required")
		return
	}
	log.Printf("INFO: lookup user %s", computeID)
	url := fmt.Sprintf("%s/user/%s", svc.UserInfoURL, computeID)
	raw, err := svc.serviceGet(url, svc.Secrets.AuthSharedSecret)
	if err != nil {
		log.Printf("ERROR: user request failed: %s", err.string())
		c.String(err.StatusCode, err.Message)
		return
	}
	c.String(http.StatusOK, string(raw))
}
