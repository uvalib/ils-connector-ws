package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type renewRequest struct {
	ComputingID string   `json:"computing_id"`
	Barcodes    []string `json:"barcodes"`
}

type renewResponseRec struct {
	Barcode       string `json:"barcode"`
	DueDate       string `json:"dueDate"`
	RenewDate     string `json:"renewalDate"`
	RecallDueDate string `json:"recallDueDate"`
	Status        string `json:"status"`
	Success       bool   `json:"success"`
	Message       string `json:"message"`
}

type renewResponse struct {
	RenewedCount int                `json:"renwed"`
	Results      []renewResponseRec `json:"results"`
}

type sirsiRenewResponse struct {
	CircRecord struct {
		Fields struct {
			CheckOutDate  string `json:"checkOutDate"`
			DueDate       string `json:"dueDate"`
			RecallDueDate string `json:"recallDueDate"`
			RenewalDate   string `json:"renewalDate"`
			Status        string `json:"status"`
		} `json:"fields"`
	} `json:"circRecord"`
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

	log.Printf("INFO: user %s requests renew %+v", v4Claims.Barcode, req.Barcodes)

	out := renewResponse{}
	for _, renewBC := range req.Barcodes {
		log.Printf("INFO: issue renew request for %s", renewBC)
		payload := struct {
			Barcode string `json:"itemBarcode"`
		}{
			Barcode: renewBC,
		}
		fields := "circRecord{checkOutDate,dueDate,renewalDate,status,recallDueDate}"
		rawRenewResp, rawErr := svc.sirsiPost(svc.HTTPClient, fmt.Sprintf("/circulation/circRecord/renew?includeFields=%s", fields), payload)
		if rawErr != nil {
			log.Printf("INFO: unable to renew %s: %s", renewBC, rawErr.Message)
			parsedErr, err := svc.handleSirsiErrorResponse(rawErr)
			if err != nil {
				log.Printf("ERROR: unable to parse sirsi failed response: %s", err.Message)
				out.Results = append(out.Results, renewResponseRec{
					Barcode: renewBC, Success: false, Message: rawErr.Message,
				})
			} else {
				reason := parsedErr.MessageList[0].Message
				log.Printf("INFO: renew %s fail reason: %s", renewBC, reason)
				out.Results = append(out.Results, renewResponseRec{
					Barcode: renewBC, Success: false, Message: reason,
				})
			}
		} else {
			log.Printf("INFO: raw renew resp: %s", rawRenewResp)
			var renewResp sirsiRenewResponse
			out.RenewedCount++
			respRec := renewResponseRec{Barcode: renewBC, Success: true}
			parseErr := json.Unmarshal(rawRenewResp, &renewResp)
			if parseErr != nil {
				log.Printf("ERROR: unable to parse renew %s response: %s", renewBC, parseErr.Error())
			} else {
				respRec.DueDate = renewResp.CircRecord.Fields.DueDate
				respRec.RenewDate = renewResp.CircRecord.Fields.RenewalDate
				respRec.RecallDueDate = renewResp.CircRecord.Fields.RecallDueDate
				respRec.Status = renewResp.CircRecord.Fields.Status
			}
			out.Results = append(out.Results, respRec)
		}
	}

	c.JSON(http.StatusOK, out)
}
