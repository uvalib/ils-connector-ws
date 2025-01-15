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
	for _, rec := range resp.Result {
		origID := idMap[rec.Key]
		respRec := validateRespRec{ID: origID, IsVideo: false, Reserve: false}
		for _, cl := range rec.Fields.CallList {
			for _, item := range cl.Fields.ItemList {
				itemType := item.Fields.ItemType.Key
				respRec.IsVideo = isVideo(itemType)
				if respRec.IsVideo == false {
					log.Printf("INFO: %s is %s cannot place on reserve", rec.Key, itemType)
					continue
				}
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
			if respRec.Reserve == true {
				break
			}
		}
		out = append(out, respRec)
	}

	log.Printf("INFO: %+v", out)
	c.JSON(http.StatusOK, out)
}
