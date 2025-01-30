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

type sirsiTransitRec struct {
	Key    string `json:"key"`
	Fields struct {
		DestinationLibrary sirsiKey `json:"destinationLibrary"`
		HoldRecord         sirsiKey `json:"holdRecord"`
	} `json:"fields"`
}

type sirsiBarcodeScanItem struct {
	Key    string `json:"key"`
	Fields struct {
		Bib struct {
			Key    string `json:"key"`
			Fields struct {
				Author string `json:"author"`
				Title  string `json:"title"`
			} `json:"fields"`
		} `json:"bib"`
		HoldRecordList []struct {
			Key    string `json:"key"`
			Fields struct {
				Patron struct {
					Key    string `json:"key"`
					Fields struct {
						DisplayName string `json:"displayName"`
						AlternateID string `json:"alternateID"`
						Barcode     string `json:"barcode"`
					} `json:"fields"`
				} `json:"patron"`
				PickupLibrary sirsiKey `json:"pickupLibrary"`
				PlacedLibrary sirsiKey `json:"placedLibrary"`
			} `json:"fields"`
		} `json:"holdRecordList"`
		Transit *sirsiTransitRec `json:"transit"`
	} `json:"fields"`
}
type sirsiUntransitResp struct {
	PrimaryAdvice   string   `json:"primaryAdvice"`
	Item            sirsiKey `json:"item"`
	CurrentStatus   string   `json:"currentStatus"`
	HoldRecord      sirsiKey `json:"holdRecord"`
	SecondaryAdvice []string `json:"secondaryAdvice"`
}

type barcodeScanResp struct {
	Title           string         `json:"title"`
	Author          string         `json:"author"`
	Barcode         string         `json:"item_id"`
	UserName        string         `json:"user_full_name"`
	UserID          string         `json:"user_id"`
	PickupLibraryID string         `json:"pickup_library"`
	ErrorMessages   []sirsiMessage `json:"error_messages,omitempty"`
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

// Fill a hold by using the provided staff session token in request.headers['SirsiSessionToken']
// - Retrieve item info with barcode
// - Untransit Item
// - Checkout item to user
// Return data to print:
// Item Title, Item Author, ItemID, User’s Name, AltID, and Delivery Location
// NOTE:
// The scan code may form the URL with ?override=OK or nothing.
// Instead, just ignore this param and always include
// override OK in the untransit request. This will work on forst try and avoid looping
// X001083728 has hold and transit
// X001167565 has nothing
func (svc *serviceContext) fillHold(c *gin.Context) {
	barcode := c.Param("barcode")
	sessionToken := c.Request.Header.Get("SirsiSessionToken")
	if sessionToken == "" {
		log.Printf("INFO: fill hold request missing session token")
		c.String(http.StatusUnauthorized, "you are not authorized for this request")
		return
	}

	out := barcodeScanResp{Barcode: barcode}
	fields := `holdRecordList{placedLibrary,pickupLibrary,patron{alternateID,displayName,barcode}},`
	fields += `bib{title,author,currentLocation},`
	fields += `transit{destinationLibrary,holdRecord{placedLibrary,pickupLibrary,patron{alternateID,displayName,barcode}}}`
	url := fmt.Sprintf("%s/catalog/item/barcode/%s?includeFields=%s", svc.SirsiConfig.WebServicesURL, barcode, fields)
	sirsiReq, _ := http.NewRequest("GET", url, nil)
	svc.setSirsiHeaders(sirsiReq, "STAFF", sessionToken)
	sirsiReq.Header.Set("SD-Working-LibraryID", "LEO")
	sirsiReq.Header.Set("x-sirs-clientID", "ILL_CKOUT")
	log.Printf("INFO: barcode scanner get item url %s with headers %+v", url, sirsiReq.Header)
	rawResp, rawErr := svc.HTTPClient.Do(sirsiReq)
	itemResp, itemErr := handleAPIResponse(url, rawResp, rawErr)
	if itemErr != nil {
		log.Printf("INFO: barcode scan item request failed: %s", itemErr.string())
		var msgs sirsiMessageList
		err := json.Unmarshal([]byte(itemErr.Message), &msgs)
		if err != nil {
			c.String(http.StatusInternalServerError, itemErr.string())
		} else {
			out.ErrorMessages = msgs.MessageList
			c.JSON(itemErr.StatusCode, out)
		}
		return
	}

	log.Printf("RAW RESP: %s", itemResp)

	var item sirsiBarcodeScanItem
	err := json.Unmarshal(itemResp, &item)
	if err != nil {
		log.Printf("ERROR: unable to parse barcode scan item response: %s", err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// X001083728 has holdREcordList
	// X004769851 has transit

	// out.Title = item.Fields.Bib.Fields.Title
	// out.Author = item.Fields.Bib.Fields.Author
	// if len(item.Fields.HoldRecordList) == 0 && item.Fields.Transit == nil {
	// 	log.Printf("INFO: no hold or tansit for %s", barcode)
	// 	out.ErrorMessages = append(out.ErrorMessages, sirsiMessage{Message: "No hold for this item."})
	// 	c.JSON(http.StatusOK, out)
	// 	return
	// }

	// firstHold := item.Fields.HoldRecordList[0]
	// out.UserName = firstHold.Fields.Patron.Fields.DisplayName
	// out.UserID = firstHold.Fields.Patron.Fields.AlternateID
	// out.PickupLibraryID = firstHold.Fields.PickupLibrary.Key

	// // if the item has transit data, untransit it
	// if item.Fields.Transit != nil {
	// 	status, err := svc.untransitItem(barcode, sessionToken, out.PickupLibraryID)
	// 	if err != nil {
	// 		log.Printf("ERROR: untransit request failed: %s", err.string())
	// 		c.String(err.StatusCode, err.Message)
	// 		return
	// 	}
	// 	if status != "ON_SHELF" {
	// 		log.Printf("ERROR: untransit error returned incorrect status %s", status)
	// 		out.ErrorMessages = append(out.ErrorMessages, sirsiMessage{Message: status})
	// 		c.JSON(http.StatusOK, out)
	// 		return
	// 	}
	// }

	// // now checkout the item....
	// // userBarcode := firstHold.Fields.Patron.Fields.Barcode

	c.String(http.StatusNotImplemented, "not implemented")
}

func (svc *serviceContext) untransitItem(barcode, sessionToken, library string) (string, *requestError) {
	log.Printf("INFO: untransit %s", barcode)
	req := struct {
		ItemBarcode string `json:"itemBarcode"`
	}{
		ItemBarcode: barcode,
	}
	payloadBytes, _ := json.Marshal(req)
	uri := "/circulation/transit/untransit"
	overrides := []string{"CKOBLOCKS", "/OK"}
	headers := make(map[string]string)
	headers["x-sirs-clientID"] = "ILL_CKOUT"
	headers["sd-working-libraryid"] = library
	headers["x-sirs-sessionToken"] = sessionToken
	log.Printf("INFO: checkout payload: %s", payloadBytes)

	sirsiResp, sirsiErr := svc.retrySirsiRequest(uri, payloadBytes, headers, overrides, "")
	if sirsiErr != nil {
		return "", sirsiErr
	}
	var untResp sirsiUntransitResp
	err := json.Unmarshal(sirsiResp, &untResp)
	if err != nil {
		log.Printf("ERROR: unable to parse untransit response: %s", err)
		re := requestError{StatusCode: http.StatusInternalServerError, Message: err.Error()}
		return "", &re
	}
	return untResp.CurrentStatus, nil
}
