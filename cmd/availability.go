package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"

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
				CallNumber     string   `json:"callNumber"`
				Volumetric     string   `json:"volumetric"`
				DispCallNumber string   `json:"dispCallNumber"`
				Classification sirsiKey `json:"classification"`
				Library        sirsiKey `json:"library"`
				Shadowed       bool     `json:"shadowed"`
				ItemList       []struct {
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
								Shadowed bool `json:"shadowed"`
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
	NonCirculating    bool             `json:"non_circulating"`
	IsVideo           bool             `json:"is_video"`
	Notice            string           `json:"notice"`
	Library           string           `json:"library"`
	LibraryID         string           `json:"library_id"`
	CurrentLocation   string           `json:"current_location"`
	CurrentLocationID string           `json:"current_location_id"`
	HomeLocationID    string           `json:"home_location_id"`
	CallNumber        string           `json:"call_number"`
	Volume            string           `json:"volume"`
	Fields            []availItemField `json:"fields"`
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
	fields := "boundWithList{*},callList{*,itemList{*,currentLocation{key,shadowed}}}"
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

	log.Printf("PARSED RESP: %+v", bibResp)
	availResp := availabilityRespoonse{
		TitleID: cleanKey,
		Columns: []string{"Library", "Current Location", "Call Number", "Barcode"},
	}

	c.JSON(http.StatusOK, availResp)
}

func (svc *serviceContext) getAvailabilityList(c *gin.Context) {
	log.Printf("INFO: get availability list")
	resp := availabilityListResponse{}
	resp.AvailabilityList.Locations = svc.Locations.Records
	resp.AvailabilityList.Libraries = svc.Libraries.Records
	c.JSON(http.StatusOK, resp)
}
