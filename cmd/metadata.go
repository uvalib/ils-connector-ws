package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
)

type sisriBibRecord struct {
	Key    string `json:"key"`
	Fields struct {
		Bib struct {
			Standard string `json:"standard"`
			Type     string `json:"type"`
			Leader   string `json:"leader"`
			Fields   []struct {
				Tag       string `json:"tag"`
				Subfields []struct {
					Code string `json:"code"`
					Data string `json:"data"`
				} `json:"subfields"`
				Inds string `json:"inds,omitempty"`
			} `json:"fields"`
		} `json:"bib"`
	} `json:"fields"`
}

//	curl --request POST  \
//		--url http://localhost:8185/metadata/u2442709/update_rights \
//		--header 'Content-Type: application/json' \
//		--data '{"resource_uri": "https://search.lib.virginia.edu/sources/uva_library/items/u2442709",
//					"name": " Copyright Undetermined", "uri": "http://rightsstatements.org/vocab/UND/1.0/"}'
func (svc *serviceContext) updateMetadataRights(c *gin.Context) {
	// NOTE: the cat_key param will be in the form u2442709 but
	// the sirsi API calls only use the numeric portion. Strip the leading 'u'
	catKey := c.Param("cat_key")
	re := regexp.MustCompile("^u")
	cleanKey := re.ReplaceAllString(catKey, "")

	var updateReq struct {
		ResourceURI string `json:"resource_uri"` // virgo URL, like https://search.lib.virginia.edu/sources/uva_library/items/u2442709
		RightsName  string `json:"name"`         // rights name
		RightsURI   string `json:"uri"`          // rights URI
	}
	err := c.ShouldBindJSON(&updateReq)
	if err != nil {
		log.Printf("ERROR: Unable to parse metadata %s update request: %s", catKey, err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("INFO: update metadata %s: %+v", catKey, updateReq)
	url := fmt.Sprintf("%s/catalog/bib/key/%s", svc.SirsiConfig.WebServicesURL, cleanKey)
	req, _ := http.NewRequest("GET", url, nil)
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	req.Header.Set("SD-Originating-App-Id", "TrackSys")
	req.Header.Set("x-sirs-clientID", "TRACKSYS")
	rawResp, rawErr := svc.HTTPClient.Do(req)
	bibBytes, bibErr := handleAPIResponse(url, rawResp, rawErr)
	if bibErr != nil {
		if bibErr.StatusCode == 404 {
			log.Printf("INFO: %s not found", catKey)
			c.String(http.StatusNotFound, fmt.Sprintf("%s not found", catKey))
		} else {
			log.Printf("WARNING: unable to load bib %s data: %s", catKey, bibErr.string())
			c.String(bibErr.StatusCode, bibErr.Message)
		}
		return
	}

	var bibRec sisriBibRecord
	parseErr := json.Unmarshal(bibBytes, &bibRec)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse sirsi bib %s response: %s", catKey, parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	// TODO update

	c.JSON(http.StatusOK, bibRec)
}
