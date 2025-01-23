package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type customInfoFields struct {
	ItemExtendedInformation sirsiKey `json:"itemExtendedInformation"`
	Data                    string   `json:"data"`
}
type customInfo struct {
	Resource string           `json:"resource"`
	Key      string           `json:"key"`
	Fields   customInfoFields `json:"fields"`
}

type sirsiItemInfo struct {
	Resource string `json:"resource"`
	Key      string `json:"key"`
	Fields   struct {
		Bib             sirsiKey  `json:"bib"`
		Call            sirsiKey  `json:"call"`
		Barcode         string    `json:"barcode"`
		Circulate       bool      `json:"circulate"`
		CopyNumber      int       `json:"copyNumber"`
		CreatedDate     string    `json:"createdDate"`
		CurrentLocation sirsiKey  `json:"currentLocation"`
		HomeLocation    sirsiKey  `json:"homeLocation"`
		ItemCategory1   *sirsiKey `json:"itemCategory1,omitempty"`
		ItemCategory2   *sirsiKey `json:"itemCategory2,omitempty"`
		ItemCategory3   *sirsiKey `json:"itemCategory3,omitempty"`
		ItemCategory4   *sirsiKey `json:"itemCategory4,omitempty"`
		ItemCategory5   *sirsiKey `json:"itemCategory5,omitempty"`
		ItemCategory6   *sirsiKey `json:"itemCategory6,omitempty"`
		ItemCategory7   *sirsiKey `json:"itemCategory7,omitempty"`
		ItemCategory8   *sirsiKey `json:"itemCategory8,omitempty"`
		ItemCategory9   *sirsiKey `json:"itemCategory9,omitempty"`
		ItemCategory10  *sirsiKey `json:"itemCategory10,omitempty"`
		CurrentLibrary  sirsiKey  `json:"currentLibrary"`
		Pickable        bool      `json:"pickable"`
		Placement       string    `json:"placement"`
		ItemType        sirsiKey  `json:"itemType"`
		LastInvDate     *string   `json:"lastInvDate,omitempty"`
		Library         sirsiKey  `json:"library"`
		MediaDesk       *string   `json:"mediaDesk,omitempty"`
		Permanent       bool      `json:"permanent"`
		Pieces          int       `json:"pieces"`
		Price           struct {
			CurrencyCode string `json:"currencyCode"`
			Amount       string `json:"amount"`
		} `json:"price"`
		Shadowed           bool         `json:"shadowed"`
		SystemModifiedDate string       `json:"systemModifiedDate"`
		TimesInventoried   int          `json:"timesInventoried"`
		CustomInformation  []customInfo `json:"customInformation"`
	} `json:"fields"`
}

type dibsData struct {
	HomeLocation sirsiKey `json:"homeLocation"`
	ItemType     sirsiKey `json:"itemType"`
}

const dibsLocationKey = "DIBS"
const dibsItemTypeKey = "DIBS"
const dibsCustomInfoKey = "DIBS-INFO"

// IN DIBS X032746483
// NO DIBS X001167565  -- changed
// NO DIBS X000394987 -- changed
func (svc *serviceContext) setBarcodeInDiBS(c *gin.Context) {
	barcode := c.Param("barcode")
	log.Printf("INFO: set barcode %s in dibs", barcode)
	item, err := svc.getDiBSItemInfo(barcode)
	if err != nil {
		log.Printf("ERROR: unable to get %s info from sirsi: %s", barcode, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data := getCustomDiBSData(item)
	if data != nil && item.Fields.ItemType.Key == dibsLocationKey {
		log.Printf("WARNING: %s is already in dibs", barcode)
		c.String(http.StatusOK, "ok")
		return
	}

	// save original info in custom data so it canbe restored later
	ci := dibsData{
		HomeLocation: sirsiKey{
			Resource: "/policy/location",
			Key:      item.Fields.HomeLocation.Key,
		},
		ItemType: sirsiKey{
			Resource: "/policy/itemType",
			Key:      item.Fields.ItemType.Key,
		},
	}
	ciBytes, _ := json.Marshal(ci)
	dibsCustom := customInfo{
		Resource: "/catalog/item/customInformation",
		Fields: customInfoFields{
			ItemExtendedInformation: sirsiKey{
				Resource: "/policy/itemExtendedInformation",
				Key:      dibsCustomInfoKey,
			},
			Data: string(ciBytes),
		},
	}

	item.Fields.HomeLocation.Key = dibsLocationKey
	item.Fields.ItemType.Key = dibsLocationKey
	item.Fields.CustomInformation = append(item.Fields.CustomInformation, dibsCustom)
	putSrr := svc.sirsiDiBSPut(item.Key, item)
	if putSrr != nil {
		log.Printf("ERROR: add to dibs failed: %s", putSrr.string())
		c.String(putSrr.StatusCode, putSrr.Message)
		return
	}

	c.String(http.StatusOK, "ok")
}

func (svc *serviceContext) setBarcodeNotInDiBS(c *gin.Context) {
	barcode := c.Param("barcode")
	log.Printf("INFO: set barcode %s not in dibs", barcode)
	item, err := svc.getDiBSItemInfo(barcode)
	if err != nil {
		log.Printf("ERROR: unable to get %s info from sirsi: %s", barcode, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data := getCustomDiBSData(item)
	if data == nil && item.Fields.ItemType.Key != dibsLocationKey {
		log.Printf("WARNING: %s is not in dibs; nothing to do", barcode)
		c.String(http.StatusOK, "ok")
		return
	}

	item.Fields.HomeLocation.Key = data.HomeLocation.Key
	item.Fields.ItemType.Key = data.ItemType.Key

	// make a new customInfo list containing all items that are not dibs
	newCI := make([]customInfo, 0)
	for _, ci := range item.Fields.CustomInformation {
		if ci.Fields.ItemExtendedInformation.Key != dibsCustomInfoKey {
			newCI = append(newCI, ci)
		}
	}
	item.Fields.CustomInformation = newCI

	putSrr := svc.sirsiDiBSPut(item.Key, item)
	if putSrr != nil {
		log.Printf("ERROR: remove from dibs failed: %s", putSrr.string())
		c.String(putSrr.StatusCode, putSrr.Message)
		return
	}

	c.String(http.StatusOK, "ok")
}

func (svc *serviceContext) checkinDiBS(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "nope")
}

func (svc *serviceContext) checkoutDiBS(c *gin.Context) {
	// TODO
	c.String(http.StatusNotImplemented, "nope")
}

func (svc *serviceContext) sirsiDiBSPut(itemKey string, data interface{}) *requestError {
	url := fmt.Sprintf("%s/catalog/item/key/%s", svc.SirsiConfig.WebServicesURL, itemKey)
	log.Printf("INFO: sirsi dibs put request: %s", url)
	startTime := time.Now()
	b, _ := json.Marshal(data)
	log.Printf("PUT PAYLOAD: %s", b)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(b))
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	req.Header.Set("x-sirs-clientID", "DIBS-STAFF")
	req.Header.Set("SD-Prompt-Return", "")
	rawResp, rawErr := svc.HTTPClient.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	log.Printf("INFO: sirsi dibs put response: %s", resp)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: %s processed in %d (ms)", url, elapsedMS)
	return err
}

func (svc *serviceContext) getDiBSItemInfo(barcode string) (*sirsiItemInfo, error) {
	fields := "*,customInformation{*}"
	url := fmt.Sprintf("/catalog/item/barcode/%s?includeFields=%s", barcode, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		return nil, fmt.Errorf("unabel to get %s info: %s", barcode, sirsiErr.Message)
	}

	var itemInfo sirsiItemInfo
	parseErr := json.Unmarshal(sirsiRaw, &itemInfo)
	if parseErr != nil {
		return nil, fmt.Errorf("unable to parse item response for %s: %s", barcode, parseErr.Error())
	}

	return &itemInfo, nil
}

func getCustomDiBSData(item *sirsiItemInfo) *dibsData {
	data := ""
	for _, ci := range item.Fields.CustomInformation {
		log.Printf("%+v", ci)
		if ci.Fields.ItemExtendedInformation.Key == dibsCustomInfoKey {
			data = ci.Fields.Data
			break
		}
	}
	if data == "" {
		return nil
	}
	var dd dibsData
	parseErr := json.Unmarshal([]byte(data), &dd)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse custom dibs data: %s", parseErr.Error())
		return nil
	}
	return &dd
}
