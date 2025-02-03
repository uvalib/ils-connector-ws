package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

type availabilityListResponse struct {
	AvailabilityList struct {
		Libraries []libraryRec  `json:"libraries"`
		Locations []locationRec `json:"locations"`
	} `json:"availability_list"`
}

type sirsiBoundWithRec struct {
	Fields struct {
		Author     string   `json:"author"`
		Bib        sirsiKey `json:"bib"`
		CallNumber string   `json:"callNumber"`
		Title      string   `json:"title"`
	} `json:"fields"`
}

type marcTag struct {
	Tag       string `json:"tag"`
	Subfields []struct {
		Code string `json:"code"`
		Data string `json:"data"`
	} `json:"subfields"`
	Inds string `json:"inds,omitempty"`
}

type sirsiBibData struct {
	Leader string    `json:"leader"`
	Fields []marcTag `json:"fields"`
}

type sirsiBibResponse struct {
	Key    string `json:"key"`
	Fields struct {
		MarcRecord sirsiBibData `json:"bib"`
		CallList   []struct {
			Key    string `json:"key"`
			Fields struct {
				Bib            sirsiKey `json:"bib"`
				Volumetric     string   `json:"volumetric"`
				DispCallNumber string   `json:"dispCallNumber"`
				Library        struct {
					Key    string           `json:"key"`
					Fields sirsiDescription `json:"fields"`
				} `json:"library"`
				Shadowed bool `json:"shadowed"`
				ItemList []struct {
					Key    string `json:"key"`
					Fields struct {
						Barcode         string `json:"barcode"`
						CopyNumber      int    `json:"copyNumber"`
						CurrentLocation struct {
							Key    string `json:"key"`
							Fields struct {
								Description string `json:"description"`
								Shadowed    bool   `json:"shadowed"`
							} `json:"fields"`
						} `json:"currentLocation"`
						HomeLocation sirsiKey `json:"homeLocation"`
						ItemType     sirsiKey `json:"itemType"`
						Shadowed     bool     `json:"shadowed"`
					} `json:"fields"`
				} `json:"itemList"`
			} `json:"fields"`
		} `json:"callList"`
		BoundWithList []struct {
			Fields struct {
				ChildList []sirsiBoundWithRec `json:"childList"`
				Parent    sirsiBoundWithRec   `json:"parent"`
			} `json:"fields"`
		} `json:"boundWithList"`
	} `json:"fields"`
}

type copyNumRec struct {
	Barcode    string `json:"barcode"`
	CopyNumber string `json:"copyNumber"`
}

type availItemField struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Visibile bool   `json:"visible"`
	Type     string `json:"type"`
}

type availItem struct {
	Barcode           string           `json:"barcode"`
	OnShelf           bool             `json:"on_shelf"`
	Unavailable       bool             `json:"unavailable"`
	Notice            string           `json:"notice"`
	Fields            []availItemField `json:"fields"`
	Library           string           `json:"library"`
	LibraryID         string           `json:"library_id"`
	CurrentLocation   string           `json:"current_location"`
	CurrentLocationID string           `json:"current_location_id"`
	HomeLocationID    string           `json:"home_location_id"`
	CallNumber        string           `json:"call_number"`
	IsVideo           bool             `json:"is_video"`
	Volume            string           `json:"volume"`
	NonCirculating    bool             `json:"non_circulating"`
	CopyNumber        int              `json:"-"`
}

func (ai *availItem) toHoldableItem() holdableItem {
	cn := ai.CallNumber
	if ai.LibraryID != "SPEC-COLL" {
		cn = strings.Split(ai.CallNumber, " (copy")[0]
	}
	return holdableItem{Barcode: ai.Barcode,
		Label: cn, Library: ai.Library,
		Location: ai.CurrentLocation, LocationID: ai.CurrentLocationID,
		IsVideo: ai.IsVideo, Notice: ai.Notice}
}

type availRequestOption struct {
	Type           string         `json:"type"`
	SignInRequired bool           `json:"sign_in_required"`
	ButtonLabel    string         `json:"button_label"`
	Description    string         `json:"description"`
	ItemOptions    []holdableItem `json:"item_options"`
	CreateURL      string         `json:"create_url"`
}

type boundWithRec struct {
	IsParent   bool   `json:"is_parent"`
	TitleID    string `json:"title_id"`
	CallNumber string `json:"call_number"`
	Title      string `json:"title"`
	Author     string `json:"author"`
}

type holdableItem struct {
	Barcode    string `json:"barcode"`
	Label      string `json:"label"`
	Library    string `json:"library"`
	Location   string `json:"location"`
	LocationID string `json:"location_id"`
	IsVideo    bool   `json:"is_video"`
	Notice     string `json:"notice"`
}

type availabilityResponse struct {
	TitleID        string               `json:"title_id"`
	Columns        []string             `json:"columns"`
	Items          []availItem          `json:"items"`
	RequestOptions []availRequestOption `json:"request_options"`
	BoundWith      []boundWithRec       `json:"bound_with"`
}

// u2419229
func (svc *serviceContext) getAvailability(c *gin.Context) {
	catKey := c.Param("cat_key")

	matched, _ := regexp.MatchString(`^u\d*$`, catKey)
	if !matched {
		log.Printf("INFO: key %s not in sirsi", catKey)
		c.String(http.StatusNotFound, fmt.Sprintf("%s not found", catKey))
		return
	}

	re := regexp.MustCompile("^u")
	cleanKey := re.ReplaceAllString(catKey, "")
	log.Printf("INFO: get availability for %s", catKey)
	fields := "boundWithList{*},bib,callList{dispCallNumber,volumetric,shadowed,library{description},"
	fields += "itemList{barcode,copyNumber,shadowed,itemType{key},homeLocation{key},currentLocation{key,description,shadowed}}}"
	url := fmt.Sprintf("/catalog/bib/key/%s?includeFields=%s", cleanKey, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		if sirsiErr.StatusCode == 404 {
			log.Printf("INFO: %s was not found", catKey)
			c.String(http.StatusNotFound, fmt.Sprintf("%s not found", catKey))
		} else {
			log.Printf("ERROR: unable to get bib info for %s: %s", catKey, sirsiErr.Message)
			c.String(sirsiErr.StatusCode, sirsiErr.Message)
		}
		return
	}

	var bibResp sirsiBibResponse
	parseErr := json.Unmarshal(sirsiRaw, &bibResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse sirsi bib response for %s: %s", catKey, parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	availResp := availabilityResponse{
		TitleID: bibResp.Key,
		Columns: []string{"Library", "Current Location", "Call Number", "Barcode"},
	}

	availResp.Items = svc.processAvailabilityItems(bibResp)
	availResp.BoundWith = svc.processBoundWithItems(bibResp)
	availResp.RequestOptions = svc.generateRequestOptions(c.GetString("jwt"), availResp.TitleID, availResp.Items, bibResp.Fields.MarcRecord)

	out := struct {
		Availability availabilityResponse `json:"availability"`
	}{
		Availability: availResp,
	}

	c.JSON(http.StatusOK, out)
}

func (svc *serviceContext) processAvailabilityItems(bibResp sirsiBibResponse) []availItem {
	log.Printf("INFO: process items for %s", bibResp.Key)
	out := make([]availItem, 0)
	for _, callRec := range bibResp.Fields.CallList {
		if callRec.Fields.Shadowed {
			continue
		}
		for _, itemRec := range callRec.Fields.ItemList {
			currLoc := svc.Locations.find(itemRec.Fields.CurrentLocation.Key)
			if currLoc.Shadowed || currLoc.Online {
				continue
			}

			item := availItem{CallNumber: callRec.Fields.DispCallNumber}
			multipleCopies := false
			for _, test := range callRec.Fields.ItemList {
				if test.Fields.CopyNumber > 1 {
					multipleCopies = true
					break
				}
			}
			if multipleCopies {
				item.CallNumber += fmt.Sprintf(" (copy %d)", itemRec.Fields.CopyNumber)
			}

			item.CopyNumber = itemRec.Fields.CopyNumber
			item.Barcode = itemRec.Fields.Barcode
			item.Volume = callRec.Fields.Volumetric
			item.Library = callRec.Fields.Library.Fields.Description
			item.LibraryID = callRec.Fields.Library.Key

			item.CurrentLocationID = itemRec.Fields.CurrentLocation.Key
			item.CurrentLocation = itemRec.Fields.CurrentLocation.Fields.Description
			item.HomeLocationID = itemRec.Fields.HomeLocation.Key
			item.Notice = svc.getItemNotice(item)
			item.IsVideo = isVideo(itemRec.Fields.ItemType.Key)
			item.OnShelf = svc.isOnShelf(item)
			item.Unavailable = svc.Locations.isUnavailable(item.CurrentLocationID)
			item.NonCirculating = svc.isNonCirculating(item)

			var fields []availItemField
			fields = append(fields, availItemField{Name: "Library", Value: item.Library, Visibile: true, Type: "text"})
			fields = append(fields, availItemField{Name: "Current Location", Value: item.CurrentLocation, Visibile: true, Type: "text"})
			fields = append(fields, availItemField{Name: "Call Number", Value: item.CallNumber, Visibile: true, Type: "text"})
			fields = append(fields, availItemField{Name: "Barcode", Value: item.Barcode, Visibile: true, Type: "text"})
			item.Fields = fields

			out = append(out, item)
		}
	}
	return out
}

func (svc *serviceContext) processBoundWithItems(bibResp sirsiBibResponse) []boundWithRec {
	// sample: sources/uva_library/items/u3315175
	log.Printf("INFO: process bound with for %s", bibResp.Key)
	out := make([]boundWithRec, 0)
	if len(bibResp.Fields.BoundWithList) > 0 {
		bwParent := extractBoundWithRec(bibResp.Fields.BoundWithList[0].Fields.Parent)
		bwParent.IsParent = true
		out = append(out, bwParent)
		for _, child := range bibResp.Fields.BoundWithList[0].Fields.ChildList {
			out = append(out, extractBoundWithRec(child))
		}
	}
	return out
}

func (svc *serviceContext) generateRequestOptions(userJWT string, titleID string, items []availItem, marc sirsiBibData) []availRequestOption {
	out := make([]availRequestOption, 0)
	holdableItems := make([]holdableItem, 0)
	var atoItem availItem

	for _, item := range items {
		// track available to order items for later use
		if item.CurrentLocation == "Available to Order" && atoItem.CurrentLocationID == "" {
			atoItem = item
		}

		// unavailable or non circulating items are not holdable. This assumes (per original code)
		// that al users can request onshelf items
		if item.Unavailable || item.NonCirculating || item.CopyNumber > 1 {
			continue
		}

		holdableItem := item.toHoldableItem()
		if svc.Locations.isMediumRare(item.HomeLocationID) {
			holdableItem.Label += " (Ivy limited circulation)"
		}
		if holdableExists(item, holdableItems) == false {
			holdableItems = append(holdableItems, holdableItem)
		}
	}

	if len(holdableItems) > 0 {
		log.Printf("INFO: add hold options for %s", titleID)
		out = append(out, availRequestOption{Type: "hold", SignInRequired: true,
			ButtonLabel: "Request item",
			Description: "Request an unavailable item or request delivery.",
			ItemOptions: holdableItems,
		})

		nonVideo := make([]holdableItem, 0)
		videos := make([]holdableItem, 0)
		for _, item := range holdableItems {
			if item.IsVideo == false {
				nonVideo = append(nonVideo, item)
			} else {
				videos = append(videos, item)
			}
		}
		if len(nonVideo) > 0 {
			log.Printf("INFO: add scan options for %s", titleID)
			out = append(out, availRequestOption{Type: "scan", SignInRequired: true,
				ButtonLabel: "Request a scan",
				Description: "Select a portion of this item to be scanned.",
				ItemOptions: nonVideo,
			})
		}

		if len(videos) > 0 {
			log.Printf("INFO: add video reserve options for %s", titleID)
			out = append(out, availRequestOption{Type: "videoReserve", SignInRequired: true,
				ButtonLabel: "Video reserve request",
				Description: "Request a video reserve for streaming.",
				ItemOptions: videos,
			})
		}
	}

	if atoItem.CurrentLocationID != "" {
		log.Printf("INFO: add available to order option")
		url := fmt.Sprintf("%s/check/%s", svc.PDAURL, titleID)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Golang_ILS_Connector")
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", userJWT))
		_, err := svc.sendRequest("pda-ws", svc.HTTPClient, req)
		if err != nil {
			if err.StatusCode == 404 {
				out = append(out, availRequestOption{Type: "pda", SignInRequired: true,
					ButtonLabel: "Order this Item",
					Description: `<div class="pda-about">Learn more about <a href="https://library.virginia.edu/about-available-to-order-items" aria-label="Available to Order" style="text-decoration: underline;" target="_blank" title="Available to Order (Opens in a new window.)" class="piwik_link">Available to Order</a> items.</div>`,
					ItemOptions: make([]holdableItem, 0),
					CreateURL:   svc.generatePDACreateURL(titleID, atoItem.Barcode, marc),
				})
			} else {
				log.Printf("ERROR: pda check failed %d - %s", err.StatusCode, err.Message)
			}
		} else {
			// success here means the item has been orderd, but sirsi not yet updated
			out = append(out, availRequestOption{Type: "pda", SignInRequired: true,
				Description: `<div class="pda-about">This item is now on order. Learn more about <a href="https://library.virginia.edu/about-available-to-order-items" aria-label="Available to Order" style="text-decoration: underline;" target="_blank" title="Available to Order (Opens in a new window.)" class="piwik_link">Available to Order</a> items.</div>`,
				ItemOptions: make([]holdableItem, 0),
			})
		}
	}

	return out
}

func holdableExists(item availItem, holdableItems []holdableItem) bool {
	exist := false
	for _, hi := range holdableItems {
		if strings.ToUpper(hi.Label) == strings.ToUpper(item.CallNumber) || hi.Barcode == item.Barcode {
			exist = true
			break
		}
	}
	if exist == false {
		// NOTES: even if call / barcode is unique, the item may be considered the same if it does not
		// have any volume info. Ex: u5841451 is a video with 2 unique callnumns:
		// VIDEO .DVD19571 and KLAUS DVD #1224. Since neiter is a different volume they are considered
		// the same item from a request persective. Only add the first instance of such items.
		return item.Volume == "" && len(holdableItems) > 0
	}
	return exist
}

func (svc *serviceContext) generatePDACreateURL(titleID, barcode string, marc sirsiBibData) string {
	pdaURL := fmt.Sprintf("%s/orders?barcode=%s&catalog_key=%s", svc.PDAURL, barcode, titleID)
	pdaURL += fmt.Sprintf("&fund_code=%s", getMarcValue(marc, "985", "first"))
	padHoldLib := getMarcValue(marc, "949", "h")
	pdaURL += fmt.Sprintf("&hold_library=%s", svc.Libraries.lookupPDALibrary(padHoldLib))
	pdaURL += fmt.Sprintf("&isbn=%s", getMarcValue(marc, "911", "a"))
	pdaURL += fmt.Sprintf("&loan_type=%s", getMarcValue(marc, "985", "last"))
	title := getMarcValue(marc, "245", "all")
	pdaURL += fmt.Sprintf("&title=%s", url.QueryEscape(title))
	return pdaURL
}

func getMarcValue(marc sirsiBibData, tag, code string) string {
	out := ""
	for _, mf := range marc.Fields {
		if mf.Tag == tag {
			if code == "first" {
				out = mf.Subfields[0].Data
			} else if code == "last" {
				out = mf.Subfields[len(mf.Subfields)-1].Data
			} else if code == "all" {
				var vals []string
				for _, sf := range mf.Subfields {
					vals = append(vals, sf.Data)
				}
				out = strings.Join(vals, " ")
			} else {
				for _, sf := range mf.Subfields {
					if sf.Code == code {
						out = sf.Data
					}
				}
			}
			break
		}
	}
	return out
}

func extractBoundWithRec(sirsiRec sirsiBoundWithRec) boundWithRec {
	return boundWithRec{Author: sirsiRec.Fields.Author,
		Title:      sirsiRec.Fields.Title,
		CallNumber: sirsiRec.Fields.CallNumber,
		TitleID:    sirsiRec.Fields.Bib.Key,
	}
}

func isVideo(itemTypeID string) bool {
	videos := []string{"VIDEOJRNL", "VIDEO-DVD", "VIDEO-DISC", "VIDEO-CASS", "RSRV-VID4", "RSRV-VID24"}
	vid := false
	for _, val := range videos {
		if val == itemTypeID {
			vid = true
		}
	}
	return vid
}

func (svc *serviceContext) getItemNotice(item availItem) string {
	if svc.Locations.isIvyStacks((item.HomeLocationID)) {
		return `Part or all of this collection is housed in <a href="https://library.virginia.edu/locations/ivy" target="_blank">Ivy Stacks</a> and requires 72 hours notice to retrieve.`
	}
	if svc.Locations.isMediumRare((item.HomeLocationID)) {
		return svc.Locations.mediumRareMessage()
	}

	if svc.Locations.isCourseReserve((item.CurrentLocationID)) {
		crURL := fmt.Sprintf("%s/course_reserves?item_id=%s", svc.SirsiConfig.ScriptURL, item.Barcode)
		req, _ := http.NewRequest("GET", crURL, nil)
		rawResp, crErr := svc.sendRequest("sirsi-scripts", svc.HTTPClient, req)
		if crErr != nil {
			log.Printf("ERROR: unable to get course reser info for %s: %s", item.Barcode, crErr.Message)
			return ""
		}

		var crResponse []struct {
			Barcode    string `json:"itemID"`
			CourseID   string `json:"courseID"`
			CourseName string `json:"courseName"`
			Instructor string `json:"instructor"`
		}
		parsErr := json.Unmarshal(rawResp, &crResponse)
		if parsErr != nil {
			log.Printf("ERROR: unable to parse course_reserve response: %s", parsErr.Error())
			return ""
		}

		if len(crResponse) > 0 && crResponse[0].CourseID != "" {
			resp := []string{"This item is on course reserves so is available for limited use through the circulation desk."}
			resp = append(resp, fmt.Sprintf("Course Name: %s", crResponse[0].CourseName))
			resp = append(resp, fmt.Sprintf("Course ID: %s", crResponse[0].CourseID))
			if crResponse[0].Instructor != "" {
				resp = append(resp, fmt.Sprintf("Instructor: %s", crResponse[0].Instructor))
				return strings.Join(resp, "\n")
			}
		}
	}

	return ""
}

func (svc *serviceContext) isNonCirculating(item availItem) bool {
	lib := svc.Libraries.find(item.LibraryID)
	loc := svc.Locations.find(item.CurrentLocationID)
	return lib != nil && loc != nil && lib.Circulating == false || loc.Circulating == false
}

func (svc *serviceContext) isOnShelf(item availItem) bool {
	lib := svc.Libraries.find(item.LibraryID)
	loc := svc.Locations.find(item.CurrentLocationID)
	return lib != nil && loc != nil && lib.OnShelf && loc.OnShelf
}

func (svc *serviceContext) getAvailabilityList(c *gin.Context) {
	log.Printf("INFO: get availability list")
	resp := availabilityListResponse{}
	resp.AvailabilityList.Locations = svc.Locations.Records
	resp.AvailabilityList.Libraries = svc.Libraries.Records
	c.JSON(http.StatusOK, resp)
}
