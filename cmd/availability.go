package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

type sirsiBibResponse struct {
	Key    string `json:"key"`
	Fields struct {
		CallList []struct {
			Key    string `json:"key"`
			Fields struct {
				Bib            sirsiKey `json:"bib"`
				Volumetric     string   `json:"volumetric"`
				DispCallNumber string   `json:"dispCallNumber"`
				Classification sirsiKey `json:"classification"`
				Library        struct {
					Key    string           `json:"key"`
					Fields sirsiDescription `json:"fields"`
				} `json:"library"`
				Shadowed bool `json:"shadowed"`
				ItemList []struct {
					Key    string `json:"key"`
					Fields struct {
						Bib             sirsiKey `json:"bib"`
						Call            sirsiKey `json:"call"`
						Barcode         string   `json:"barcode"`
						Circulate       bool     `json:"circulate"`
						CopyNumber      int      `json:"copyNumber"`
						CurrentLocation struct {
							Key    string `json:"key"`
							Fields struct {
								Description string `json:"description"`
								Shadowed    bool   `json:"shadowed"`
							} `json:"fields"`
						} `json:"currentLocation"`
						HomeLocation sirsiKey `json:"homeLocation"`
						ItemType     sirsiKey `json:"itemType"`
						Library      sirsiKey `json:"library"`
						MediaDesk    string   `json:"mediaDesk"`
						Shadowed     bool     `json:"shadowed"`
					} `json:"fields"`
				} `json:"itemList"`
			} `json:"fields"`
		} `json:"callList"`
		BoundWithList []struct {
			Fields struct {
				ChildList []struct {
					Fields struct {
						Author     string   `json:"author"`
						Bib        sirsiKey `json:"bib"`
						CallNumber string   `json:"callNumber"`
						Title      string   `json:"title"`
					} `json:"fields"`
				} `json:"childList"`
				Parent struct {
					Fields struct {
						Author     string   `json:"author"`
						Bib        sirsiKey `json:"bib"`
						CallNumber string   `json:"callNumber"`
						Title      string   `json:"title"`
					} `json:"fields"`
				} `json:"parent"`
			} `json:"fields"`
		} `json:"boundWithList"`
	} `json:"fields"`
}

type availItemField struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Visibile bool   `json:"visible"`
	Type     string `json:"text"`
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
}

type availItemOptions struct {
	Barcode    string `json:"barcode"`
	Label      string `json:"label"`
	Library    string `json:"library"`
	Location   string `json:"location"`
	LocationID string `json:"location_id"`
	IsVideo    bool   `json:"is_video"`
	Notice     string `json:"notice"`
}

type availRequestOption struct {
	Type           string             `json:"type"`
	SignInRequired bool               `json:"sign_in_required"`
	ButtonLabel    string             `json:"button_label"`
	Description    string             `json:"description"`
	ItemOptions    []availItemOptions `json:"item_options"`
}

type availabilityRespoonse struct {
	TitleID        string               `json:"title_id"`
	Columns        []string             `json:"columns"`
	Items          []availItem          `json:"items"`
	RequestOptions []availRequestOption `json:"request_options"`
}

// u2419229
func (svc *serviceContext) getAvailability(c *gin.Context) {
	catKey := c.Param("cat_key")
	re := regexp.MustCompile("^u")
	cleanKey := re.ReplaceAllString(catKey, "")
	log.Printf("INFO: get availability for %s", catKey)
	fields := "boundWithList{*},callList{*,library{description},itemList{*,currentLocation{key,description,shadowed}}}"
	url := fmt.Sprintf("/catalog/bib/key/%s?includeFields=%s", cleanKey, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		if sirsiErr.StatusCode == 404 {
			log.Printf("INFO: %s was not found", catKey)
			c.String(http.StatusNotFound, fmt.Sprintf("%s not found", catKey))
		} else {
			log.Printf("ERROR: unable to get bin info for %s: %s", catKey, sirsiErr.Message)
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

	availResp := availabilityRespoonse{
		TitleID: bibResp.Key,
		Columns: []string{"Library", "Current Location", "Call Number", "Barcode"},
	}

	for _, callRec := range bibResp.Fields.CallList {
		for _, itemRec := range callRec.Fields.ItemList {
			item := availItem{CallNumber: callRec.Fields.DispCallNumber}
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
			item.NonCirculating = false

			var fields []availItemField
			fields = append(fields, availItemField{Name: "Library", Value: item.Library, Visibile: true, Type: "text"})
			fields = append(fields, availItemField{Name: "Current Location", Value: item.CurrentLocation, Visibile: true, Type: "text"})
			fields = append(fields, availItemField{Name: "Call Number", Value: item.CallNumber, Visibile: true, Type: "text"})
			fields = append(fields, availItemField{Name: "Barcode", Value: item.Barcode, Visibile: true, Type: "text"})
			item.Fields = fields

			availResp.Items = append(availResp.Items, item)
		}
	}

	out := struct {
		Availability availabilityRespoonse `json:"availability"`
	}{
		Availability: availResp,
	}

	c.JSON(http.StatusOK, out)
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
		return "This item does not circulate outside of library spaces. When you request this item from Ivy, it will be delivered to the Small Special Collections Library for you to use in the reading room only."
	}

	// https://ilstest.lib.virginia.edu/uhtbin/course_reserves?item_id=35007007757960 this has a reserve
	// items without a reserve return [{"instructor":null,"itemID":"X031517421","courseName":null,"courseID":null}]
	crURL := fmt.Sprintf("%s/course_reserves?item_id=%s", svc.SirsiConfig.ScriptURL, item.Barcode)
	rawResp, crErr := svc.serviceGet(crURL, "")
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

	return ""
}

func (svc *serviceContext) isOnShelf(item availItem) bool {
	lib := svc.Libraries.find(item.LibraryID)
	loc := svc.Locations.find(item.CurrentLocationID)
	return lib.OnShelf && loc.OnShelf
}

func (svc *serviceContext) getAvailabilityList(c *gin.Context) {
	log.Printf("INFO: get availability list")
	resp := availabilityListResponse{}
	resp.AvailabilityList.Locations = svc.Locations.Records
	resp.AvailabilityList.Libraries = svc.Libraries.Records
	c.JSON(http.StatusOK, resp)
}
