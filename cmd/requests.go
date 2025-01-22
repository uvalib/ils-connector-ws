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

type holdRequest struct {
	PickupLibrary string `json:"pickupLibrary"`
	ItemBarcode   string `json:"itemBarcode"`
	IlliadTN      string `json:"illiadTN"` // only present in scan requests (illiad transaction ID)
}

type holdResponse struct {
	Hold holdResponseData `json:"hold"`
}

type holdErrorsData struct {
	Sirsi       []string `json:"sirsi"`
	ItemBarcode []string `json:"item_barcode"`
}

type holdResponseData struct {
	PickupLibrary string          `json:"pickupLibrary"`
	ItemBarcode   string          `json:"itemBarcode"`
	UserID        string          `json:"user_id"`
	Errors        *holdErrorsData `json:"errors,omitempty"`
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
	var holdReq holdRequest
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

	out := holdResponse{Hold: holdResponseData{
		ItemBarcode:   holdReq.ItemBarcode,
		PickupLibrary: holdReq.PickupLibrary,
		UserID:        v4Claims.UserID,
	}}

	holdErr := svc.placeHold(holdReq, v4Claims.Barcode, v4Claims.HomeLibrary)
	if holdErr != nil {
		log.Printf("ERROR: patron %s place hold %+v failed: %s", v4Claims.Barcode, holdReq, holdErr.string())
		out.Hold.Errors = getHoldErrorMessages(holdErr.Message)
	}

	c.JSON(http.StatusOK, out)
}

func getHoldErrorMessages(rawErrors string) *holdErrorsData {
	errors := holdErrorsData{}
	var errMessages sirsiMessageList
	parseErr := json.Unmarshal([]byte(rawErrors), &errMessages)
	if parseErr != nil {
		errors.Sirsi = append(errors.Sirsi, parseErr.Error())
	} else {
		for _, m := range errMessages.MessageList {
			if m.Code == "keyParseError" {
				errors.ItemBarcode = append(errors.ItemBarcode, "Invalid title key")
			} else {
				errors.Sirsi = append(errors.Sirsi, m.Message)
			}
		}
	}
	return &errors
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
	var holdReq holdRequest
	err := c.ShouldBindJSON(&holdReq)
	if err != nil {
		log.Printf("INFO: Unable to parse scan request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	v4Claims, claimErr := getVirgoClaims(c)
	if claimErr != nil {
		c.String(http.StatusUnauthorized, "you are not authorized to request a scan")
		return
	}

	out := holdResponse{Hold: holdResponseData{
		ItemBarcode:   holdReq.ItemBarcode,
		PickupLibrary: holdReq.PickupLibrary,
		UserID:        v4Claims.UserID,
	}}

	log.Printf("INFO: scan request: %+v", holdReq)
	holdErr := svc.placeHold(holdReq, "999999462", "LEO")
	if holdErr != nil {
		log.Printf("ERROR: patron %s scan request %+v failed: %s", v4Claims.Barcode, holdReq, holdErr.string())
		out.Hold.Errors = getHoldErrorMessages(holdErr.Message)
	}

	c.JSON(http.StatusOK, out)
}

func (svc *serviceContext) placeHold(holdReq holdRequest, patronBarcode, workLibrary string) *requestError {
	log.Printf("INFO: place patron %s hold request: %+v", patronBarcode, holdReq)
	req := sirsiHoldRequest{
		Type:         "TITLE",
		Range:        "GROUP",
		RecallStatus: "STANDARD",
		PickupLibrary: sirsiKey{
			Resource: "/policy/library",
			Key:      holdReq.PickupLibrary,
		},
		ItemBarcode:   holdReq.ItemBarcode,
		PatronBarcode: patronBarcode,
		Comment:       holdReq.IlliadTN,
	}
	payloadBytes, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/circulation/holdRecord/placeHold?includeFields=holdRecord{*}", svc.SirsiConfig.WebServicesURL)
	log.Printf("INFO: post request %s with payload %s", url, payloadBytes)
	postReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(postReq, "PATRON", svc.SirsiSession.SessionToken)
	postReq.Header.Set("sd-working-libraryid", workLibrary)
	rawResp, rawErr := svc.HTTPClient.Do(postReq)
	_, holdErr := handleAPIResponse(url, rawResp, rawErr)
	if holdErr != nil {
		return holdErr
	}
	log.Printf("INFO: hold placed")
	return nil
}

func (svc *serviceContext) fillHold(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "not implemented")
}
