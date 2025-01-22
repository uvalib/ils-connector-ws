package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type renewRequest struct {
	ComputingID string `json:"computing_id"`
	Barcode     string `json:"item_barcode"`
}

type renewResponseRec struct {
	Barcode string `json:"barcode"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type renewResponse struct {
	RenewedCount int                `json:"renwed"`
	Results      []renewResponseRec `json:"results"`
}

type sirsiCheckoutBarcodesResponse struct {
	Fields struct {
		CircRecordList []struct {
			Fields struct {
				Item struct {
					Fields struct {
						Barcode string `json:"barcode"`
					} `json:"fields"`
				} `json:"item"`
			} `json:"fields"`
		} `json:"circRecordList"`
	} `json:"fields"`
}

func (svc *serviceContext) renewCheckouts(c *gin.Context) {
	var req renewRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		log.Printf("INFO: Unable to parse hold request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	v4Claims, claimErr := getVirgoClaims(c)
	if claimErr != nil {
		c.String(http.StatusUnauthorized, "you are not authorized to issue a renew request")
		return
	}
	log.Printf("INFO: user %s issues a renew request for %s; retrieve all checkouts first", v4Claims.UserID, req.Barcode)
	fields := "circRecordList{item{barcode}}"
	url := fmt.Sprintf("/user/patron/alternateID/%s?i&includeFields=%s", v4Claims.UserID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.SlowHTTPClient, url)
	if sirsiErr != nil {
		log.Printf("ERROR: get %s checkouts failed: %s", v4Claims.UserID, sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}
	var checkoutsResp sirsiCheckoutBarcodesResponse
	parseErr := json.Unmarshal(sirsiRaw, &checkoutsResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse checkouts response: %s", parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	out := renewResponse{}
	requested := false
	for _, rec := range checkoutsResp.Fields.CircRecordList {
		coBarcode := rec.Fields.Item.Fields.Barcode
		if req.Barcode == "all" || coBarcode == req.Barcode {
			log.Printf("INFO: issue renew request for %s", coBarcode)
			requested = true
			payload := struct {
				Barcode string `json:"itemBarcode"`
			}{
				Barcode: coBarcode,
			}
			rawRenewResp, rawErr := svc.sirsiPost(svc.HTTPClient, "/circulation/circRecord/renew", payload)
			if rawErr != nil {
				log.Printf("INFO: unable to renew %s: %s", coBarcode, rawErr.Message)
				out.Results = append(out.Results, renewResponseRec{
					Barcode: coBarcode, Success: false, Message: rawErr.Message,
				})
			} else {
				log.Printf("INFO: raw renew resp: %s", rawRenewResp)
				out.RenewedCount++
				out.Results = append(out.Results, renewResponseRec{Barcode: coBarcode, Success: true})
			}
		}
	}

	if requested == false && req.Barcode != "all" {
		out.Results = append(out.Results, renewResponseRec{
			Barcode: req.Barcode,
			Success: false,
			Message: fmt.Sprintf("Item %s not found", req.Barcode)})
	}

	c.JSON(http.StatusOK, out)
}
