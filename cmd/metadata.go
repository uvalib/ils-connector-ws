package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type marcSubField struct {
	Code string `json:"code"`
	Data string `json:"data"`
}

type marcField struct {
	Tag       string         `json:"tag"`
	Subfields []marcSubField `json:"subfields"`
	Inds      string         `json:"inds,omitempty"`
}
type sisriBibRecord struct {
	Resource string `json:"resource"`
	Key      string `json:"key"`
	Fields   struct {
		Bib struct {
			Standard string      `json:"standard"`
			Type     string      `json:"type"`
			Leader   string      `json:"leader"`
			Fields   []marcField `json:"fields"`
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
	cleanKey := cleanCatKey(catKey)

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
	bibBytes, bibErr := svc.sendRequest("sirsi", svc.HTTPClient, req)
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

	marcRights := marcField{Tag: "856", Inds: "41"}
	marcRights.Subfields = append(marcRights.Subfields, marcSubField{Code: "r", Data: updateReq.RightsURI})
	marcRights.Subfields = append(marcRights.Subfields, marcSubField{Code: "t", Data: updateReq.RightsName})
	marcRights.Subfields = append(marcRights.Subfields, marcSubField{Code: "u", Data: updateReq.ResourceURI})
	marcRights.Subfields = append(marcRights.Subfields, marcSubField{Code: "e", Data: "(dpeaa) UVA TrackSys"})

	// see if 856 field from tracksys is already present
	existFieldIdx := 0
	newIdx := -1
	for idx, bf := range bibRec.Fields.Bib.Fields {
		if newIdx == -1 {
			tagNum, _ := strconv.Atoi(bf.Tag)
			if tagNum >= 856 {
				newIdx = idx
			}
		}
		if bf.Tag == "856" {
			for _, sf := range bf.Subfields {
				if sf.Code == "e" {
					match, _ := regexp.MatchString(`uva tracksys`, strings.ToLower(sf.Data))
					if match {
						existFieldIdx = idx
						break
					}
				}
			}
		}
	}

	if existFieldIdx > 0 {
		log.Printf("INFO: data already has tracksys user right data in field %d", existFieldIdx)
		bibRec.Fields.Bib.Fields[existFieldIdx] = marcRights
	} else {
		if newIdx > -1 {
			log.Printf("INFO: insert rights at index %d", newIdx)
			bibRec.Fields.Bib.Fields = slices.Insert(bibRec.Fields.Bib.Fields, newIdx, marcRights)
		} else {
			log.Printf("INFO: append rights after last field")
			bibRec.Fields.Bib.Fields = append(bibRec.Fields.Bib.Fields, marcRights)
		}
	}

	// cleanup leader
	// https://www.oclc.org/bibformats/en/fixedfield/elvl.html
	leader17 := bibRec.Fields.Bib.Leader[17]
	matched, _ := regexp.MatchString(`[A-Z]`, string(leader17))
	if matched {
		leader := bibRec.Fields.Bib.Leader
		bibRec.Fields.Bib.Leader = leader[:17] + " " + leader[18:]
	}

	payloadBytes, _ := json.Marshal(bibRec)
	url = fmt.Sprintf("%s/catalog/bib/key/%s", svc.SirsiConfig.WebServicesURL, cleanKey)
	req, _ = http.NewRequest("PUT", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	req.Header.Set("SD-Originating-App-Id", "TrackSys")
	req.Header.Set("x-sirs-clientID", "TRACKSYS")
	_, putErr := svc.sendRequest("sirsi", svc.HTTPClient, req)
	if putErr != nil {
		log.Printf("ERROR: update rights failed: %s", putErr.string())
		c.String(putErr.StatusCode, putErr.Message)
		return
	}

	c.JSON(http.StatusOK, bibRec)
}
