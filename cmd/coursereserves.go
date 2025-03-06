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

type sirsiSearchRec struct {
	Key    string `json:"key"`
	Fields struct {
		CallList []struct {
			Key    string `json:"key"`
			Fields struct {
				ItemList []struct {
					Key    string `json:"key"`
					Fields struct {
						ItemType struct {
							Key string `json:"key"`
						} `json:"itemType"`
						Library struct {
							Key string `json:"key"`
						} `json:"library"`
					} `json:"fields"`
				} `json:"itemList"`
			} `json:"fields"`
		} `json:"callList"`
	} `json:"fields"`
}

type sirsiBibSearchResp struct {
	TotalResults int              `json:"totalResults"`
	Result       []sirsiSearchRec `json:"result"`
}

type validateRespRec struct {
	ID      string `json:"id"`
	Reserve bool   `json:"reserve"`
	IsVideo bool   `json:"is_video"`
}

func (svc *serviceContext) validateCourseReserves(c *gin.Context) {
	var req struct {
		Items []string `json:"items"`
	}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		log.Printf("INFO: Unable to parse validate reserves request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	v4Claims, _ := getVirgoClaims(c)
	log.Printf("INFO: patron %s requests validation course reserve for %v", v4Claims.UserID, req.Items)

	idMap := make(map[string]string)
	var bits []string
	keyCleanRegEx := regexp.MustCompile("^u")
	for _, key := range req.Items {
		cleanKey := keyCleanRegEx.ReplaceAllString(key, "")
		idMap[cleanKey] = key
		bits = append(bits, fmt.Sprintf("%s{CKEY}", cleanKey))
	}
	keys := strings.Join(bits, " OR ")
	query := fmt.Sprintf("GENERAL:\"%s\"", keys)
	fields := "callList{itemList{itemType,library}}"
	uri := fmt.Sprintf("/catalog/bib/search?includeFields=%s&q=%s&ct=%d", fields, url.QueryEscape(query), len(req.Items))
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, uri)
	if sirsiErr != nil {
		log.Printf("ERROR: reserve item lookup failed: %s", sirsiErr.Message)
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}

	var resp sirsiBibSearchResp
	parseErr := json.Unmarshal(sirsiRaw, &resp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse search response: %s", parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	out := make([]validateRespRec, 0)
	for cleanKey, origID := range idMap {
		respRec := validateRespRec{ID: origID, IsVideo: false, Reserve: false}

		// find the item in the sirsi response if possible
		var rec *sirsiSearchRec
		for _, item := range resp.Result {
			if item.Key == cleanKey {
				rec = &item
			}
		}
		if rec == nil {
			log.Printf("INFO: %s not found in sirsi", origID)
		} else {
			for _, cl := range rec.Fields.CallList {
				for _, item := range cl.Fields.ItemList {
					itemType := item.Fields.ItemType.Key
					respRec.IsVideo = isVideo(itemType)
					if respRec.IsVideo == true {
						log.Printf("INFO: %s is video (%s) and may be a candidate for reserve", rec.Key, itemType)
						lib := rec.Fields.CallList[0].Fields.ItemList[0].Fields.Library.Key
						if lib == "HEALTHSCI" || lib == "SPEC-COLL" {
							log.Printf("INFO: cannot reserve %s: invalid library %s", respRec.ID, lib)
						} else if lib == "LAW" && itemType == "VIDEO-DVD" {
							log.Printf("INFO: cannot reserve %s: %s from %s", respRec.ID, itemType, lib)
						} else {
							log.Printf("INFO: reserve %s type %s from library %s is ok", respRec.ID, itemType, lib)
							respRec.Reserve = true
							break
						}
					}
				}
				if respRec.Reserve == true {
					break
				}
			}
		}

		// for rejected or non-video items, look them up in solr and determine if
		// they are actually a video/streaming video and flag correctly
		// (sirsi have enout info to determine this completely)
		if respRec.IsVideo == false || respRec.Reserve == false {
			log.Printf("INFO: sirsi data has video %t and reserve %t; check solr doc", respRec.IsVideo, respRec.Reserve)
			solrDoc, err := svc.getSolrDoc(respRec.ID)
			if err != nil {
				log.Printf("ERROR: unable to get solr doc for %s: %s", respRec.ID, err.Error())
			} else {
				if (solrDoc.Pool[0] == "video" && listContains(solrDoc.Location, "Internet materials")) || listContains(solrDoc.Source, "Avalon") {
					log.Printf("INFO: per solr document, %s is a video", respRec.ID)
					respRec.IsVideo = true
					respRec.Reserve = true
				}
			}
		}
		out = append(out, respRec)
	}

	c.JSON(http.StatusOK, out)
}
