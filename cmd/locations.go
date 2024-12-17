package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

type locationContext struct {
	Records        []locationRec
	NonCirculating []string
	OnShelf        []string
	RefreshAt      time.Time
}

type sirsiLocationRec struct {
	Key    string `json:"key"`
	Fields struct {
		PolicyNumber int    `json:"policyNumber"`
		Description  string `json:"description"`
		Shadowed     bool   `json:"shadowed"`
	} `json:"fields"`
}

type locationRec struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Description string `json:"description"`
	Online      bool   `json:"online"`
	Shadowed    bool   `json:"shadowed"`
	OnShelf     bool   `json:"on_shelf"`
	Circulating bool   `json:"circulating"`
}

func (svc *serviceContext) refreshLocations() {
	if len(svc.Locations.NonCirculating) == 0 {
		log.Printf("INFO: load non-circulating location data")
		svc.Locations.NonCirculating = loadDataFile("./data/noncirc-loc.txt")
	}
	if len(svc.Locations.OnShelf) == 0 {
		log.Printf("INFO: load on shelf location data")
		svc.Locations.OnShelf = loadDataFile("./data/onshelf-loc.txt")
	}

	locs, err := svc.getSirsiLocations()
	if err != nil {
		log.Printf("ERROR: get locations failed: %s", err.Error())
		svc.Locations.Records = make([]locationRec, 0)
		svc.Locations.RefreshAt = time.Now()
		return
	}

	svc.Locations.Records = locs
	svc.Locations.RefreshAt = time.Now().Add(24 * time.Hour)
}

func (svc *serviceContext) getSirsiLocations() ([]locationRec, error) {
	log.Printf("INFO: get sirsi locations")
	locs := make([]locationRec, 0)
	url := fmt.Sprintf("/policy/location/simpleQuery?key=*&includeFields=key,policyNumber,description,shadowed")
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		return locs, fmt.Errorf("%s", sirsiErr.string())
	}

	var locResp []sirsiLocationRec
	parseErr := json.Unmarshal(sirsiRaw, &locResp)
	if parseErr != nil {
		return locs, fmt.Errorf("unable to parse reponse: %s", parseErr.Error())
	}

	for _, sl := range locResp {
		loc := locationRec{ID: sl.Fields.PolicyNumber}
		loc.Key = sl.Key
		loc.Description = sl.Fields.Description
		loc.OnShelf = svc.Locations.isOnShelfLocation(sl.Key)
		loc.Circulating = !svc.Locations.isNonCirculatingLocation(sl.Key)
		loc.Online = svc.Locations.isOnlineLocation(sl.Key)
		loc.Shadowed = sl.Fields.Shadowed

		locs = append(locs, loc)
	}

	return locs, nil
}

func (lc *locationContext) findLocation(key string) *locationRec {
	var match *locationRec
	for _, loc := range lc.Records {
		if loc.Key == key {
			match = &loc
			break
		}
	}
	return match
}

func (lc *locationContext) isOnShelfLocation(key string) bool {
	match := false
	for _, loc := range lc.OnShelf {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isNonCirculatingLocation(key string) bool {
	match := false
	for _, loc := range lc.NonCirculating {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isOnlineLocation(key string) bool {
	online := false
	for _, loc := range lc.onlineLocations() {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			online = true
			break
		}
	}
	return online
}

func (lc *locationContext) isUnavailableLocation(key string) bool {
	online := false
	for _, loc := range lc.unavailableLocations() {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			online = true
			break
		}
	}
	return online
}

func (lc *locationContext) onlineLocations() []string {
	return []string{"INTERNET", "NOTOREPDA"}
}

func (lc *locationContext) unavailableLocations() []string {
	return []string{
		"LOST",
		"UNKNOWN",
		"MISSING",
		"DISCARD",
		"WITHDRAWN",
		"BARRED",
		"BURSARED",
		"ORD-CANCLD",
		"HEREDOC",
	}
}
