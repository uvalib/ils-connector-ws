package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"slices"

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

type sirsiHoldPatron struct {
	Key    string `json:"key"`
	Fields struct {
		DisplayName string `json:"displayName"`
		AlternateID string `json:"alternateID"`
		Barcode     string `json:"barcode"`
	}
}

type sirsiHoldRec struct {
	Key    string `json:"key"`
	Fields struct {
		Patron        sirsiHoldPatron `json:"patron"`
		RecallStatus  string          `json:"recallStatus"`
		Status        string          `json:"status"`
		PickupLibrary sirsiKey        `json:"pickupLibrary"`
		PlacedLibrary sirsiKey        `json:"placedLibrary"`
	} `json:"fields"`
}

type sirsiTransitRec struct {
	Key    string `json:"key"`
	Fields struct {
		DestinationLibrary sirsiKey  `json:"destinationLibrary"`
		HoldRecord         *sirsiKey `json:"holdRecord"`
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
		FillableHolds []sirsiHoldRec   `json:"fillableHoldList"`
		Transit       *sirsiTransitRec `json:"transit"`
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
		sirsiErr, err := svc.handleSirsiErrorResponse(holdErr)
		if err != nil {
			log.Printf("ERROR: patron %s place hold %+v failed: %s", v4Claims.Barcode, holdReq, holdErr.Message)
			c.String(holdErr.StatusCode, holdErr.Message)
			return
		}
		out.Hold.Errors = getHoldErrorMessages(sirsiErr)
		log.Printf("INFO: patron %s unable to place hold %+v: %+v", v4Claims.Barcode, holdReq, *out.Hold.Errors)
	}

	c.JSON(http.StatusOK, out)
}

func getHoldErrorMessages(sirsiErr *sirsiError) *holdErrorsData {
	errors := holdErrorsData{}
	for _, m := range sirsiErr.MessageList {
		if m.Code == "keyParseError" {
			errors.ItemBarcode = append(errors.ItemBarcode, "Invalid title key")
		} else {
			errors.Sirsi = append(errors.Sirsi, m.Message)
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
	log.Printf("%+v", hold)

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
			log.Printf("INFO: unable to cancel hold: %s", sirsiErr.Message)
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
		sirsiErr, err := svc.handleSirsiErrorResponse(holdErr)
		if err != nil {
			log.Printf("ERROR: patron %s scan request %+v failed: %s", v4Claims.Barcode, holdReq, holdErr.Message)
			c.String(holdErr.StatusCode, holdErr.Message)
			return
		}
		out.Hold.Errors = getHoldErrorMessages(sirsiErr)
		log.Printf("INFO: patron %s unable to place scan request %+v: %+v", v4Claims.Barcode, holdReq, *out.Hold.Errors)
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
	_, holdErr := svc.sendRequest("sirsi", svc.HTTPClient, postReq)
	if holdErr != nil {
		return holdErr
	}
	log.Printf("INFO: hold placed")
	return nil
}

type fillHoldInfo struct {
	Key             string
	Barcode         string
	UserName        string
	UserID          string
	UserBarcode     string
	PickupLibraryID string
	Untransit       bool
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
func (svc *serviceContext) fillHold(c *gin.Context) {
	barcode := c.Param("barcode")
	sessionToken := c.Request.Header.Get("SirsiSessionToken")
	if sessionToken == "" {
		log.Printf("INFO: fill hold request missing session token")
		c.String(http.StatusUnauthorized, "you are not authorized for this request")
		return
	}

	out := barcodeScanResp{Barcode: barcode}
	fields := `bib{title,author,currentLocation},`
	fields += `transit{destinationLibrary,holdRecord},`
	fields += `fillableHoldList{placedLibrary,pickupLibrary,patron{alternateID,displayName,barcode}}`
	url := fmt.Sprintf("%s/catalog/item/barcode/%s?includeFields=%s", svc.SirsiConfig.WebServicesURL, barcode, fields)
	sirsiReq, _ := http.NewRequest("GET", url, nil)
	svc.setSirsiHeaders(sirsiReq, "STAFF", sessionToken)
	sirsiReq.Header.Set("SD-Working-LibraryID", "LEO")
	sirsiReq.Header.Set("x-sirs-clientID", "ILL_CKOUT")
	log.Printf("INFO: barcode scanner get item url %s with headers %+v", url, sirsiReq.Header)
	itemResp, itemErr := svc.sendRequest("sirsi", svc.HTTPClient, sirsiReq)
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

	var item sirsiBarcodeScanItem
	err := json.Unmarshal(itemResp, &item)
	if err != nil {
		log.Printf("ERROR: unable to parse barcode scan item response: %s", err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// enough data present to populate more fields in the response; do so
	out.Title = item.Fields.Bib.Fields.Title
	out.Author = item.Fields.Bib.Fields.Author

	if len(item.Fields.FillableHolds) == 0 && (item.Fields.Transit == nil || (item.Fields.Transit != nil && item.Fields.Transit.Fields.HoldRecord == nil)) {
		log.Printf("INFO: no hold or transit for %s", barcode)
		out.ErrorMessages = append(out.ErrorMessages, sirsiMessage{Message: "No hold for this item."})
		c.JSON(http.StatusOK, out)
		return
	}

	// collect a list of item data to try for untransit/checkout. If a transit item is present, it will be used first
	items := make([]fillHoldInfo, 0)
	if item.Fields.Transit != nil {
		// IMPORTANT: the data returned in the transit block only contains the holdRecord KEY and no other details
		// if a transit record is found, find the hold details in the fillableHoldList
		// IF there is a transit record, the item must be untransited first, then the checkout can proceed
		var transitHold *sirsiHoldRec
		log.Printf("INFO: %s is in transit; find hold details", barcode)
		holdKey := item.Fields.Transit.Fields.HoldRecord.Key
		delIdx := -1
		for hIdx, h := range item.Fields.FillableHolds {
			if h.Key == holdKey {
				transitHold = &h
				delIdx = hIdx
				break
			}
		}
		if transitHold == nil {
			log.Printf("ERROR: unable to find in-transit hold %s in fillable holds list", holdKey)
			c.String(http.StatusInternalServerError, "unable to find hold data in transit item")
			return
		}

		items = append(items, fillHoldInfo{
			Key:             item.Fields.Transit.Fields.HoldRecord.Key,
			Barcode:         barcode,
			UserName:        transitHold.Fields.Patron.Fields.DisplayName,
			UserID:          transitHold.Fields.Patron.Fields.AlternateID,
			UserBarcode:     transitHold.Fields.Patron.Fields.Barcode,
			PickupLibraryID: transitHold.Fields.PickupLibrary.Key,
			Untransit:       true,
		})

		// now remove the hold rec for the in-transit item so it is not processed twice
		log.Printf("INFO: remove fillable hold index %d of %d for %s that was referenced by the transit record", delIdx, len(item.Fields.FillableHolds), transitHold.Key)
		item.Fields.FillableHolds = slices.Delete(item.Fields.FillableHolds, delIdx, delIdx+1)
		log.Printf("INFO: after delete, %d records remain", len(item.Fields.FillableHolds))
	}

	// collect details needed to fill hold from each rec
	for _, tgtHold := range item.Fields.FillableHolds {
		items = append(items, fillHoldInfo{
			Key:             tgtHold.Key,
			Barcode:         barcode,
			UserName:        tgtHold.Fields.Patron.Fields.DisplayName,
			UserID:          tgtHold.Fields.Patron.Fields.AlternateID,
			UserBarcode:     tgtHold.Fields.Patron.Fields.Barcode,
			PickupLibraryID: tgtHold.Fields.PickupLibrary.Key,
			Untransit:       false,
		})
	}

	log.Printf("INFO: %s has %d holds to try", barcode, len(items))
	errors := make([]sirsiMessage, 0)
	success := false
	for _, tgt := range items {
		// populate more response data based on target hold; necessary for the next steps
		out.UserName = tgt.UserName
		out.UserID = tgt.UserID
		out.PickupLibraryID = tgt.PickupLibraryID

		// if necessary, first try an untransit
		if tgt.Untransit {
			status, err := svc.fillHoldUntransitItem(tgt, sessionToken)
			if err != nil {
				log.Printf("INFO: untransit request failed: %s", err.string())
				errors = append(errors, sirsiMessage{Code: fmt.Sprintf("%d", err.StatusCode), Message: err.Message})
				continue
			}
			if status != "ON_SHELF" {
				log.Printf("INFO: untransit returned incorrect status %s", status)
				errors = append(errors, sirsiMessage{Message: status})
				c.JSON(http.StatusOK, out)
				continue
			}
		}

		coErr := svc.fillHoldCheckout(tgt, sessionToken)
		if coErr != nil {
			// on failure, there willl be errors listed in the error string. parse and save
			log.Printf("INFO: fill hold checkout for %s failed: %s", barcode, coErr.string())
			var msgs sirsiMessageList
			json.Unmarshal([]byte(coErr.Message), &msgs)
			errors = append(errors, msgs.MessageList...)
		} else {
			success = true
			break
		}
	}

	if success == false {
		out.ErrorMessages = errors
	}

	c.JSON(http.StatusOK, out)
}

func (svc *serviceContext) fillHoldUntransitItem(tgt fillHoldInfo, sessionToken string) (string, *requestError) {
	log.Printf("INFO: untransit %s[%s]", tgt.Barcode, tgt.Key)
	req := struct {
		ItemBarcode string `json:"itemBarcode"`
	}{
		ItemBarcode: tgt.Barcode,
	}
	payloadBytes, _ := json.Marshal(req)
	uri := "/circulation/transit/untransit"
	overrides := []string{"CKOBLOCKS", "/OK"}
	headers := make(map[string]string)
	headers["x-sirs-clientID"] = "ILL_CKOUT"
	headers["sd-working-libraryid"] = tgt.PickupLibraryID
	headers["x-sirs-sessionToken"] = sessionToken
	log.Printf("INFO: untransit payload: %s", payloadBytes)

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
	log.Printf("INFO: %s[%s] untransit request results in status: %s", tgt.Barcode, tgt.Key, untResp.CurrentStatus)
	return untResp.CurrentStatus, nil
}

func (svc *serviceContext) fillHoldCheckout(tgt fillHoldInfo, sessionToken string) *requestError {
	log.Printf("INFO: fillhold checkout %s[%s] from user %s", tgt.Barcode, tgt.Key, tgt.UserID)
	req := struct {
		PatronBarcode string `json:"patronBarcode"`
		ItemBarcode   string `json:"itemBarcode"`
	}{
		PatronBarcode: tgt.UserBarcode,
		ItemBarcode:   tgt.Barcode,
	}
	payloadBytes, _ := json.Marshal(req)
	uri := "/circulation/circRecord/checkOut"
	overrides := []string{"CKOBLOCKS"}
	headers := make(map[string]string)
	headers["x-sirs-clientID"] = "ILL_CKOUT"
	headers["sd-working-libraryid"] = tgt.PickupLibraryID
	headers["x-sirs-sessionToken"] = sessionToken
	log.Printf("INFO: fillhold checkout payload: %s", payloadBytes)
	_, sirsiErr := svc.retrySirsiRequest(uri, payloadBytes, headers, overrides, "")
	if sirsiErr != nil {
		return sirsiErr
	}
	log.Printf("INFO: fillhold checkout %s[%s] was successful", tgt.Barcode, tgt.Key)
	return nil
}
