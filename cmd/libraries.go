package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

type libraryContext struct {
	Records        []libraryRec
	NonCirculating []string
	OnShelf        []string
	RefreshAt      time.Time
}

type sirsiLibraryResp struct {
	Key    string `json:"key"`
	Fields struct {
		PolicyNumber int    `json:"policyNumber"`
		Description  string `json:"description"`
	} `json:"fields"`
}

type libraryRec struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Description string `json:"description"`
	OnShelf     bool   `json:"on_shelf"`
	Circulating bool   `json:"circulating"`
}

func (svc *serviceContext) refreshLibraries() {
	log.Printf("INFO: get sirsi libraries")
	svc.Libraries.Records = make([]libraryRec, 0)

	if len(svc.Libraries.NonCirculating) == 0 {
		log.Printf("INFO: load non-circulating library data")
		svc.Libraries.NonCirculating = loadDataFile("./data/noncirc-lib.txt")
	}
	if len(svc.Libraries.OnShelf) == 0 {
		log.Printf("INFO: load on shelf library data")
		svc.Libraries.OnShelf = loadDataFile("./data/onshelf-lib.txt")
	}

	url := fmt.Sprintf("/policy/library/simpleQuery?key=*&includeFields=key,policyNumber,description")
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		log.Printf("ERROR: get libraries failed: %s", sirsiErr.string())
		svc.Libraries.RefreshAt = time.Now()
		return
	}

	var libResp []sirsiLibraryResp
	parseErr := json.Unmarshal(sirsiRaw, &libResp)
	if parseErr != nil {
		log.Printf("ERROR: parse libraries response failed: %s", parseErr.Error())
		svc.Libraries.RefreshAt = time.Now()
		return
	}

	for _, sl := range libResp {
		lib := libraryRec{ID: sl.Fields.PolicyNumber,
			Key:         sl.Key,
			Description: sl.Fields.Description,
		}
		lib.OnShelf = svc.Libraries.isOnShelfLibrary(sl.Key)
		lib.Circulating = !svc.Locations.isNonCirculatingLocation(sl.Key)
		svc.Libraries.Records = append(svc.Libraries.Records, lib)
	}
}

func (lc *libraryContext) isNonCirculatingLibrary(key string) bool {
	match := false
	for _, loc := range lc.NonCirculating {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *libraryContext) isOnShelfLibrary(key string) bool {
	match := false
	for _, loc := range lc.OnShelf {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}
