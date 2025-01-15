package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type holdRequest struct {
	PickupLibrary string `json:"pickupLibrary"`
	ItemBarcode   string `json:"itemBarcode"`
}

func (svc *serviceContext) createHold(c *gin.Context) {
	var holdReq holdRequest
	err := c.ShouldBindJSON(&holdReq)
	if err != nil {
		log.Printf("INFO: Unable to parse hold request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	c.String(http.StatusNotImplemented, "not implemented")
}

func (svc *serviceContext) deleteHold(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "not implemented")
}

func (svc *serviceContext) createScan(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "not implemented")
}

func (svc *serviceContext) fillHold(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "not implemented")
}
