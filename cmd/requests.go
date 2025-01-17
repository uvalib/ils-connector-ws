package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
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

type sirsiHoldRec struct {
	Key    string `json:"key"`
	Fields struct {
		Patron struct {
			Fields struct {
				AlternateID string `json:"alternateID"`
			} `json:"fields"`
		} `json:"patron"`
		RecallStatus string `json:"recallStatus"`
		Status       string `json:"status"`
	} `json:"fields"`
}

func (svc *serviceContext) createHold(c *gin.Context) {
	var holdReq holdData
	err := c.ShouldBindJSON(&holdReq)
	if err != nil {
		log.Printf("INFO: Unable to parse hold request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	v4Claims, claimErr := getVirgoClaims(c)
	if claimErr != nil {
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
	holdID := c.Param("id")
	v4Claims, claimErr := getVirgoClaims(c)
	if claimErr != nil {
		c.String(http.StatusUnauthorized, "you are not authorized to cancel a hold")
		return
	}
	log.Printf("INFO: %s requests hold %s cancel", v4Claims.UserID, holdID)

	fields := "status,recallStatus,patron{alternateID}"
	url := fmt.Sprintf("/circulation/holdRecord/key/%s?includeFields=%s", holdID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		if sirsiErr.StatusCode == 404 {
			log.Printf("INFO: %s was not found", holdID)
			c.String(http.StatusNotFound, fmt.Sprintf("%s not found", holdID))
		} else {
			log.Printf("ERROR: unable to get hold info for %s: %s", holdID, sirsiErr.Message)
			c.String(sirsiErr.StatusCode, sirsiErr.Message)
		}
		return
	}

	var hold sirsiHoldRec
	parseErr := json.Unmarshal(sirsiRaw, &hold)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse sirsi hold response for %s: %s", holdID, parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	holdOwner := hold.Fields.Patron.Fields.AlternateID
	if strings.ToUpper(holdOwner) != strings.ToUpper(v4Claims.UserID) {
		log.Printf("ERROR: hold user mismatch user %s vs hold patron %s", v4Claims.UserID, holdOwner)
		c.String(http.StatusBadRequest, "you do not hold this item")
		return
	}

	if (hold.Fields.Status == "PLACED" && hold.Fields.RecallStatus != "RUSH") == false {
		log.Printf("INFO: hold %s cannot be cancelled", holdID)
		c.String(http.StatusBadRequest, "hold cannot be cancelled")
		return
	}

	delURL := fmt.Sprintf("/circulation/holdRecord/key/%s", holdID)
	_, sirsiErr = svc.sirsiDelete(svc.HTTPClient, delURL)
	if sirsiErr != nil {
		if sirsiErr.StatusCode == 204 {
			c.String(http.StatusOK, "deleted")
		} else {
			log.Printf("ERROR: cancel hold failed: %s", sirsiErr.string())
			c.String(sirsiErr.StatusCode, sirsiErr.Message)
		}
		return
	}
	c.String(http.StatusOK, "deleted")
}

func (svc *serviceContext) createScan(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "not implemented")
}

func (svc *serviceContext) fillHold(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "not implemented")
}
