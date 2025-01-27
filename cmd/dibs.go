package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

type dibsUserCheckouts struct {
	Fields struct {
		Barcode        string `json:"barcode"`
		CircRecordList []struct {
			Key    string `json:"key"`
			Fields struct {
				Item struct {
					Key    string `json:"key"`
					Fields struct {
						Barcode string `json:"barcode"`
					} `json:"fields"`
				} `json:"item"`
				Library sirsiKey `json:"library"`
			} `json:"fields"`
		} `json:"circRecordList"`
	} `json:"fields"`
}

// for checkIn and checkOut when response code is 400
type sirsiCheckoutError struct {
	MessageList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messageList"`
	DescribeURI    string `json:"describeUri"`
	PromptRequired bool   `json:"promptRequired"`
	DataMap        struct {
		PromptType         string `json:"promptType"`
		RecommendedAction  string `json:"recommendedAction"`
		PromptRequiresData bool   `json:"promptRequiresData"`
		PromptDataType     string `json:"promptDataType"`
	} `json:"dataMap"`
}

type dibsData struct {
	HomeLocation sirsiKey `json:"homeLocation"`
	ItemType     sirsiKey `json:"itemType"`
}

type dibsItemRequest struct {
	Duration string `json:"duration"`
	UserID   string `json:"user_id"`
	Barcode  string `json:"barcode"`
}

type dibsCheckoutInfo struct {
	UserBarcode string
	ItemBarcode string
	CheckedOut  bool
	LibraryID   string
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
	putSrr := svc.sirsiUpdateDiBSStatus(item.Key, item)
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

	putSrr := svc.sirsiUpdateDiBSStatus(item.Key, item)
	if putSrr != nil {
		log.Printf("ERROR: remove from dibs failed: %s", putSrr.string())
		c.String(putSrr.StatusCode, putSrr.Message)
		return
	}

	c.String(http.StatusOK, "ok")
}

func (svc *serviceContext) checkinDiBS(c *gin.Context) {
	var req struct {
		Barcode string `json:"barcode"`
	}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		log.Printf("INFO: unable to parse dibs checkin request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	v4Claims, claimErr := getVirgoClaims(c)
	if claimErr != nil {
		c.String(http.StatusUnauthorized, "you are not authorized to issue a dibs checkout request")
		return
	}
	log.Printf("INFO: user %s requests dibs checkin of %s", v4Claims.UserID, req.Barcode)

	// ensure item is checked out
	info, valErr := svc.getDiBSCheckoutInfo(v4Claims.UserID, req.Barcode)
	if valErr != nil {
		log.Printf("INFO: unable to validate %s dibs checkin request: %s", v4Claims.UserID, valErr.Error())
		c.String(http.StatusBadRequest, valErr.Error())
		return
	}

	if info.CheckedOut == false {
		log.Printf("INFO: %s requests dibs checkin and item is not checked out", v4Claims.UserID)
		c.String(http.StatusOK, "ok")
		return
	}

	// NOTES: dont loop the checkin attempt.. just do it 1x and fail with logs
	ciReq := struct {
		ItemBarcode string `json:"itemBarcode"`
	}{
		ItemBarcode: req.Barcode,
	}
	payloadBytes, _ := json.Marshal(ciReq)
	url := fmt.Sprintf("%s/circulation/circRecord/checkIn?includeFields={*}", svc.SirsiConfig.WebServicesURL)
	sirsiReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(sirsiReq, "STAFF", svc.SirsiSession.SessionToken)
	sirsiReq.Header.Set("x-sirs-clientID", "DIBS-PATRN")
	sirsiReq.Header.Set("sd-working-libraryid", "UVA-LIB")
	sirsiReq.Header.Set("SD-Prompt-Return", "")
	rawResp, rawErr := svc.HTTPClient.Do(sirsiReq)
	_, ciErr := handleAPIResponse(url, rawResp, rawErr)
	if ciErr != nil {
		var msgs sirsiMessageList
		err := json.Unmarshal([]byte(ciErr.Message), &msgs)
		if err != nil {
			c.String(http.StatusInternalServerError, ciErr.string())
		} else {
			outErr := struct {
				Errors []sirsiMessage `json:"errors"`
			}{
				Errors: msgs.MessageList,
			}
			c.JSON(ciErr.StatusCode, outErr)
		}
		return
	}

	c.String(http.StatusOK, "ok")
}

// paload from dibs/lsp.py: f'{{"duration": "{duration}", "user_id" : "{username}", "barcode" : "{barcode}"}}'
func (svc *serviceContext) checkoutDiBS(c *gin.Context) {
	var req dibsItemRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		log.Printf("INFO: unable to parse dibs checkout request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	v4Claims, claimErr := getVirgoClaims(c)
	if claimErr != nil {
		c.String(http.StatusUnauthorized, "you are not authorized to issue a dibs checkout request")
		return
	}
	durationHr, timeErr := strconv.ParseInt(req.Duration, 10, 64)
	if timeErr != nil {
		log.Printf("ERROR: invalid duration passed to dibs checkout request %+v: %s", req, timeErr.Error())
		c.String(http.StatusBadRequest, "invalid duration")
		return
	}
	dueDate := time.Now().Local()
	dueDate = dueDate.Add(time.Duration(durationHr) * time.Hour)
	iso8601Due := dueDate.Format(time.RFC3339) // all other formats fail validation

	log.Printf("INFO: user %s requests dibs checkout of %s for %s hours", v4Claims.UserID, req.Barcode, req.Duration)
	info, valErr := svc.getDiBSCheckoutInfo(v4Claims.UserID, req.Barcode)
	if valErr != nil {
		log.Printf("INFO: unable to validate %s dibs checkout request: %s", v4Claims.UserID, valErr.Error())
		c.String(http.StatusBadRequest, valErr.Error())
		return
	}

	// already checked out, nothing to do
	if info.CheckedOut == true {
		log.Printf("INFO: %s requests dibs checkout and item is already checked out", v4Claims.UserID)
		c.String(http.StatusOK, "ok")
		return
	}

	coReq := struct {
		ItemBarcode       string   `json:"itemBarcode"`
		PatronBarcode     string   `json:"patronBarcode"`
		DueDate           string   `json:"dueDate"`
		ReserveCollection sirsiKey `json:"reserveCollection"`
	}{
		ItemBarcode:   info.ItemBarcode,
		PatronBarcode: info.UserBarcode,
		DueDate:       iso8601Due,
		ReserveCollection: sirsiKey{
			Resource: "/policy/reserveCollection",
			Key:      "DIBS-E-RES",
		},
	}
	payloadBytes, _ := json.Marshal(coReq)
	url := fmt.Sprintf("%s/circulation/circRecord/checkOut?includeFields={*}", svc.SirsiConfig.WebServicesURL)
	log.Printf("INFO: checkout url: %s,  payload: %s", url, payloadBytes)
	sirsiReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(sirsiReq, "STAFF", svc.SirsiSession.SessionToken)
	sirsiReq.Header.Set("x-sirs-clientID", "DIBS-PATRN")
	sirsiReq.Header.Set("sd-working-libraryid", "UVA-LIB")
	sirsiReq.Header.Set("SD-Prompt-Return", "CIRC_NONCHARGEABLE_OVRCD/DIBSDIBS")
	rawResp, rawErr := svc.HTTPClient.Do(sirsiReq)
	coResp, coErr := handleAPIResponse(url, rawResp, rawErr)
	if coErr != nil {
		log.Printf("INFO: checkout request failed: %s", coErr.string())
		var msgs sirsiMessageList
		err := json.Unmarshal([]byte(coErr.Message), &msgs)
		if err != nil {
			c.String(http.StatusInternalServerError, coErr.string())
		} else {
			outErr := struct {
				Errors []sirsiMessage `json:"errors"`
			}{
				Errors: msgs.MessageList,
			}
			c.JSON(coErr.StatusCode, outErr)
		}
		return
	}
	log.Printf("INFO: %s was checked out; %s", req.Barcode, coResp)
	c.String(http.StatusOK, "ok")
}

func (svc *serviceContext) sirsiUpdateDiBSStatus(itemKey string, data interface{}) *requestError {
	url := fmt.Sprintf("%s/catalog/item/key/%s", svc.SirsiConfig.WebServicesURL, itemKey)
	log.Printf("INFO: sirsi dibs update status request: %s", url)
	startTime := time.Now()
	b, _ := json.Marshal(data)
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

func (svc *serviceContext) getDiBSCheckoutInfo(computeID, barcode string) (*dibsCheckoutInfo, error) {
	fields := "barcode,circRecordList{library,item{barcode}}"
	url := fmt.Sprintf("/user/patron/alternateID/%s?i&includeFields=%s", computeID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.SlowHTTPClient, url)
	if sirsiErr != nil {
		return nil, fmt.Errorf("unable to get %s checkouts: %s", computeID, sirsiErr.string())
	}
	var userCheckouts dibsUserCheckouts
	parseErr := json.Unmarshal(sirsiRaw, &userCheckouts)
	if parseErr != nil {
		return nil, fmt.Errorf("unable to parse user checkouts response for %s: %s", computeID, parseErr.Error())
	}

	out := dibsCheckoutInfo{UserBarcode: userCheckouts.Fields.Barcode, ItemBarcode: barcode}
	for _, cr := range userCheckouts.Fields.CircRecordList {
		if cr.Fields.Item.Fields.Barcode == barcode {
			out.LibraryID = cr.Fields.Library.Key
			out.CheckedOut = true
			break
		}
	}

	return &out, nil
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
