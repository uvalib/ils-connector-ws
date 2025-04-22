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
	"github.com/google/go-querystring/query"
)

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

type availabilityListResponse struct {
	AvailabilityList struct {
		Libraries []libraryRec  `json:"libraries"`
		Locations []locationRec `json:"locations"`
	} `json:"availability_list"`
}

// NOTES: This is an internal structure that is used to track availability details
// that are obtained from two sources:
//  1. pulled from the sirsiBibResponse
//  2. parsed from the solr document response
//
// Necessary fields are pulled from here into client responses for library availability and request options
type availItem struct {
	Barcode           string
	CallNumber        string
	CopyNumber        int
	Library           string
	LibraryID         string
	CurrentLocation   string
	CurrentLocationID string
	HomeLocationID    string
	Unavailable       bool
	Notice            string
	IsVideo           bool
	Volume            string
	SCLocation        string
}

func (ai *availItem) toLibraryItem() libraryItem {
	cn := ai.CallNumber
	if ai.CopyNumber > 0 {
		cn += fmt.Sprintf(" (copy %d)", ai.CopyNumber)
	}
	return libraryItem{
		Barcode:         ai.Barcode,
		CallNumber:      cn,
		CurrentLocation: ai.CurrentLocation,
		DiBS:            (ai.HomeLocationID == "DIBS"),
		Notice:          ai.Notice}
}

func (ai *availItem) toHoldableItem(notes string) holdableItem {
	cn := ai.CallNumber
	if ai.LibraryID == "SPEC-COLL" && ai.CopyNumber > 0 {
		// copies are unique items in SC, append the copy info to callNum to make each unique
		cn += fmt.Sprintf(" (copy %d)", ai.CopyNumber)
	}
	loc := ai.HomeLocationID
	if ai.CurrentLocation == "SC-Ivy" {
		// There is special handling in in the workflow for equests from SC-Ivy.
		// Override location with this info if it is present to ensure it appears in the
		// aeon request URL
		loc = "SC-Ivy"
	}
	return holdableItem{
		Barcode:    ai.Barcode,
		CallNumber: cn,
		Location:   loc,
		Library:    ai.Library,
		SCNotes:    notes,
		Notice:     ai.Notice}
}

type boundWithRec struct {
	IsParent   bool   `json:"is_parent"`
	TitleID    string `json:"title_id"`
	CallNumber string `json:"call_number"`
	Title      string `json:"title"`
	Author     string `json:"author"`
}

type availabilityResponse struct {
	TitleID        string          `json:"title_id"`
	Libraries      []*libraryItems `json:"libraries"`
	RequestOptions []requestOption `json:"request_options"`
	BoundWith      []boundWithRec  `json:"bound_with"`
}

type libraryItem struct {
	Barcode         string `json:"barcode"`
	CallNumber      string `json:"call_number"`
	CurrentLocation string `json:"current_location"`
	DiBS            bool   `json:"dibs"`
	Notice          string `json:"notice"`
}

type libraryItems struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Items []libraryItem `json:"items"`
}

// u2419229
func (svc *serviceContext) getAvailability(c *gin.Context) {
	catKey := c.Param("cat_key")

	availResp := availabilityResponse{
		TitleID:        catKey,
		Libraries:      make([]*libraryItems, 0),
		BoundWith:      make([]boundWithRec, 0),
		RequestOptions: make([]requestOption, 0),
	}

	items := make([]availItem, 0)
	matched, _ := regexp.MatchString(`^u\d*$`, catKey)
	if !matched {
		log.Printf("INFO: key %s not in sirsi", catKey)
	} else {
		log.Printf("INFO: get availability for %s", catKey)
		notFound := false
		bibResp, sirsiErr := svc.getSirsiItem(catKey)
		if sirsiErr != nil {
			log.Printf("ERROR: get sirsi item %s failed: %s", catKey, sirsiErr.string())
			if sirsiErr.StatusCode != 404 {
				c.String(sirsiErr.StatusCode, sirsiErr.Message)
				return
			}
			log.Printf("WARN: %s was not found in sirsi", catKey)
			notFound = true
		}

		if notFound == false {
			// parse sirsi data into an easier to manage format
			items = svc.parseItemsFromSirsi(bibResp)

			availResp.TitleID = bibResp.Key
			svc.addLibraryItems(&availResp, items)
			availResp.BoundWith = svc.processBoundWithItems(bibResp)
			availResp.RequestOptions = svc.generateRequestOptions(c, availResp.TitleID, items, bibResp.Fields.MarcRecord)
		}
	}

	// Now pull the solr doc for this item and use it to add and extra options for special collections, streaming video and health science
	solrDoc, solrErr := svc.getSolrDoc(catKey)
	if solrErr != nil {
		log.Printf("ERROR: %s", solrErr.Error())
	} else {
		log.Printf("INFO: update reserve options based on solr doc")
		scItems := svc.extractSpecialCollectionsItems(&availResp, solrDoc)
		if len(scItems) > 0 {
			svc.addLibraryItems(&availResp, scItems)
			items = append(items, scItems...)
		}
		svc.addAeonRequestOptions(&availResp, solrDoc, items)

		claims, err := getVirgoClaims(c)
		if err != nil {
			log.Printf("ERROR: unable to get claims: %s", err.Error())
		} else {
			if claims.HomeLibrary == "HEALTHSCI" {
				svc.updateHSLScanOptions(solrDoc, &availResp)
			}
			if claims.CanPlaceReserve {
				svc.addStreamingVideoOption(solrDoc, &availResp)
			}
		}
	}

	c.JSON(http.StatusOK, availResp)
}

func (svc *serviceContext) parseItemsFromSirsi(bibResp *sirsiBibResponse) []availItem {
	log.Printf("INFO: process items for %s", bibResp.Key)
	out := make([]availItem, 0)
	for _, callRec := range bibResp.Fields.CallList {
		if callRec.Fields.Shadowed {
			log.Printf("INFO: callRec key %s is shadowed; not adding to availability list", callRec.Key)
			continue
		}
		for _, itemRec := range callRec.Fields.ItemList {
			if itemRec.Fields.Shadowed {
				log.Printf("INFO: itemRec key %s is shadowed; not adding to availability list", itemRec.Key)
				continue
			}
			currLoc := svc.Locations.find(itemRec.Fields.CurrentLocation.Key)
			if currLoc.Shadowed || currLoc.Online {
				log.Printf("INFO: location %s is shadowed; not adding to availability list", currLoc.Key)
				continue
			}

			item := availItem{CallNumber: callRec.Fields.DispCallNumber}
			for _, test := range callRec.Fields.ItemList {
				// if any item has copy num > 1, there are multiple copies; store the number.
				if test.Fields.CopyNumber > 1 {
					item.CopyNumber = itemRec.Fields.CopyNumber
					break
				}
			}

			item.Barcode = itemRec.Fields.Barcode
			item.Volume = callRec.Fields.Volumetric
			item.LibraryID = callRec.Fields.Library.Key
			item.Library = callRec.Fields.Library.Fields.Description
			item.CurrentLocationID = itemRec.Fields.CurrentLocation.Key
			item.CurrentLocation = itemRec.Fields.CurrentLocation.Fields.Description
			item.HomeLocationID = itemRec.Fields.HomeLocation.Key
			item.Notice = svc.getItemNotice(item)
			item.IsVideo = isVideo(itemRec.Fields.ItemType.Key)
			item.Unavailable = svc.Locations.isUnavailable(item.CurrentLocationID)
			out = append(out, item)
		}
	}
	return out
}

func (svc *serviceContext) extractSpecialCollectionsItems(result *availabilityResponse, solrDoc *solrDocument) []availItem {
	out := make([]availItem, 0)
	if solrDoc.SCAvailability == "" {
		return out
	}
	result.TitleID = solrDoc.ID

	type solrItem struct {
		Library         string `json:"library"`
		CurrentLocation string `json:"current_location"`
		CallNumber      string `json:"call_number"`
		Barcode         string `json:"barcode"`
		SCLocation      string `json:"special_collections_location"`
	}

	log.Printf("INFO: parse availability from sc_availability_large_single")
	var parsed []solrItem
	parseErr := json.Unmarshal([]byte(solrDoc.SCAvailability), &parsed)
	if parseErr != nil {
		log.Printf("ERROR: unable to  parse sc_availability_large_single: %s", parseErr.Error())
	}

	// convert solrItem to availItem adding the missing library id
	for _, si := range parsed {
		newItem := availItem{Barcode: si.Barcode, CallNumber: si.CallNumber,
			Library: si.Library, LibraryID: svc.Libraries.lookupID(si.Library),
			CurrentLocation: si.CurrentLocation, SCLocation: si.SCLocation,
		}
		out = append(out, newItem)
	}

	return out
}

func (svc *serviceContext) addLibraryItems(result *availabilityResponse, items []availItem) {
	for _, item := range items {
		var tgtLib *libraryItems
		for _, tgt := range result.Libraries {
			if tgt.ID == item.LibraryID {
				tgtLib = tgt
				break
			}
		}
		if tgtLib == nil {
			newLib := libraryItems{
				ID:    item.LibraryID,
				Name:  item.Library,
				Items: make([]libraryItem, 0)}
			tgtLib = &newLib
			result.Libraries = append(result.Libraries, tgtLib)
		}
		tgtLib.Items = append(tgtLib.Items, item.toLibraryItem())
	}
}

func openURLQuery(baseURL string, doc *solrDocument) string {
	var req struct {
		Action  string `url:"Action"`
		Form    string `url:"Form"`
		ISSN    string `url:"issn,omitempty"`
		Title   string `url:"loantitle"`
		Author  string `url:"loanauthor,omitempty"`
		Edition string `url:"loanedition,omitempty"`
		Volume  string `url:"photojournalvolume,omitempty"`
		Issue   string `url:"photojournalissue,omitempty"`
		Date    string `url:"loandate,omitempty"`
	}
	req.Action = "10"
	req.Form = "21"
	req.ISSN = strings.Join(doc.ISSN, ", ")
	req.Title = strings.Join(doc.Title, "; ")
	req.Author = strings.Join(doc.Author, "; ")
	req.Edition = doc.Edition
	req.Volume = doc.Volume
	req.Issue = doc.Issue
	req.Date = doc.PublicationDate
	query, err := query.Values(req)
	if err != nil {
		log.Printf("ERROR: couldn't generate OpenURL: %s", err.Error())
	}

	return fmt.Sprintf("%s/illiad.dll?%s", baseURL, query.Encode())
}

func (svc *serviceContext) getSirsiItem(catKey string) (*sirsiBibResponse, *requestError) {
	cleanKey := cleanCatKey(catKey)
	fields := "boundWithList{*},bib,callList{dispCallNumber,volumetric,shadowed,library{description},"
	fields += "itemList{barcode,copyNumber,shadowed,itemType{key},homeLocation{key},currentLocation{key,description,shadowed}}}"
	url := fmt.Sprintf("/catalog/bib/key/%s?includeFields=%s", cleanKey, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.SlowHTTPClient, url)
	if sirsiErr != nil {
		return nil, sirsiErr
	}

	var bibResp sirsiBibResponse
	parseErr := json.Unmarshal(sirsiRaw, &bibResp)
	if parseErr != nil {
		re := requestError{
			StatusCode: http.StatusInternalServerError,
			Message:    fmt.Sprintf("unable to parse sirsi response: %s", parseErr.Error())}
		return nil, &re
	}
	return &bibResp, nil
}

func (svc *serviceContext) processBoundWithItems(bibResp *sirsiBibResponse) []boundWithRec {
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
		rawResp, crErr := svc.sendRequest("sirsi", svc.HTTPClient, req)
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

func (svc *serviceContext) getAvailabilityList(c *gin.Context) {
	log.Printf("INFO: get availability list")
	resp := availabilityListResponse{}
	resp.AvailabilityList.Locations = svc.Locations.Records
	resp.AvailabilityList.Libraries = svc.Libraries.Records
	c.JSON(http.StatusOK, resp)
}
