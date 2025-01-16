package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/uvalib/virgo4-jwt/v4jwt"
)

type holdData struct {
	PickupLibrary string `json:"pickupLibrary"`
	ItemBarcode   string `json:"itemBarcode"`
	UserID        string `json:"user_id"`
}

type sirsiHoldRequest struct {
	Type          string   `json:"holdType"`
	Range         string   `json:"holdRange"`
	RecallStatus  string   `json:"recallStatus"`
	PickupLibrary sirsiKey `json:"pickupLibrary"`
	ItemBarcode   string   `json:"itemBarcode"`
	PatronBarcode string   `json:"patronBarcode"`
	Comment       string   `json:"comment"`
}

func (svc *serviceContext) createHold(c *gin.Context) {
	var holdReq holdData
	err := c.ShouldBindJSON(&holdReq)
	if err != nil {
		log.Printf("INFO: Unable to parse hold request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	claims, exist := c.Get("claims")
	if exist == false {
		log.Printf("ERROR: no claims found for user requesting a hold")
		c.String(http.StatusUnauthorized, "you are not authorized to request a hold")
		return
	}
	v4Claims, ok := claims.(*v4jwt.V4Claims)
	if !ok {
		log.Printf("ERROR: invalid claims found for user requesting a hold")
		c.String(http.StatusUnauthorized, "you are not authorized to request a hold")
		return
	}
	log.Printf("INFO: hold request: %+v", holdReq)

	req := sirsiHoldRequest{
		Type:         "TITLE",
		Range:        "GROUP",
		RecallStatus: "STANDARD",
		PickupLibrary: sirsiKey{
			Resource: "/policy/library",
			Key:      holdReq.PickupLibrary,
		},
		ItemBarcode:   holdReq.ItemBarcode,
		PatronBarcode: v4Claims.Barcode,
	}
	payloadBytes, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/circulation/holdRecord/placeHold?includeFields=holdRecord{*}", svc.SirsiConfig.WebServicesURL)
	postReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(postReq, "PATRON", svc.SirsiSession.SessionToken)
	postReq.Header.Set("sd-working-libraryid", v4Claims.HomeLibrary)
	rawResp, rawErr := svc.HTTPClient.Do(postReq)
	_, holdErr := handleAPIResponse(url, rawResp, rawErr)
	if holdErr != nil {
		log.Printf("ERROR: place hold %+v failed: %s", holdReq, holdErr.Message)
		c.String(holdErr.StatusCode, holdErr.Message)
		return
	}

	// to client:  {"hold":{"pickup_library":"CLEMONS","item_barcode":"X001167565","user_id":"lf6f"}}
	out := struct {
		Hold holdData `json:"hold"`
	}{
		Hold: holdData{
			ItemBarcode:   holdReq.ItemBarcode,
			PickupLibrary: holdReq.PickupLibrary,
			UserID:        v4Claims.UserID,
		},
	}

	c.JSON(http.StatusOK, out)
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
