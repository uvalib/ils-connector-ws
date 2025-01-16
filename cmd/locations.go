package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

type locationContext struct {
	Records          []locationRec
	ReserveLocations []string
	NonCirculating   []string
	OnShelf          []string
	RefreshAt        time.Time
}

type sirsiReserveLocationRec struct {
	Key    string `json:"key"`
	Fields struct {
		Location struct {
			Key string `json:"key"`
		} `json:"location"`
	} `json:"fields"`
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

	svc.Locations.RefreshAt = time.Now().Add(24 * time.Hour)
	svc.getSirsiLocations()
	svc.getSirsiReserveLocations()
	log.Printf("INFO: locations refreshed")
}

func (svc *serviceContext) getSirsiLocations() {
	log.Printf("INFO: get sirsi locations")
	svc.Locations.Records = make([]locationRec, 0)
	url := fmt.Sprintf("/policy/location/simpleQuery?key=*&includeFields=key,policyNumber,description,shadowed")
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		log.Printf("ERROR: unable to get locations: %s", sirsiErr.Message)
		svc.Locations.RefreshAt = time.Now()
		return
	}

	var locResp []sirsiLocationRec
	parseErr := json.Unmarshal(sirsiRaw, &locResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse locations response: %s", parseErr)
		svc.Locations.RefreshAt = time.Now()
		return
	}

	for _, sl := range locResp {
		loc := locationRec{ID: sl.Fields.PolicyNumber}
		loc.Key = sl.Key
		loc.Description = sl.Fields.Description
		loc.OnShelf = svc.Locations.isOnShelf(sl.Key)
		loc.Circulating = !svc.Locations.isNonCirculating(sl.Key)
		loc.Online = svc.Locations.isOnline(sl.Key)
		loc.Shadowed = sl.Fields.Shadowed
		svc.Locations.Records = append(svc.Locations.Records, loc)
	}
}

func (svc *serviceContext) getSirsiReserveLocations() {
	log.Printf("INFO: get sirsi reserve locations")
	svc.Locations.ReserveLocations = make([]string, 0)
	url := fmt.Sprintf("/policy/reserveCollection/simpleQuery?key=*&includeFields=key,location{key}")
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		log.Printf("ERROR: unable to get reserve locations: %s", sirsiErr.Message)
		svc.Locations.RefreshAt = time.Now()
		return
	}

	var locResp []sirsiReserveLocationRec
	parseErr := json.Unmarshal(sirsiRaw, &locResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse reserve locations response: %s", parseErr)
		svc.Locations.RefreshAt = time.Now()
		return
	}

	for _, l := range locResp {
		svc.Locations.ReserveLocations = append(svc.Locations.ReserveLocations, l.Fields.Location.Key)
	}
}

func (lc *locationContext) find(key string) *locationRec {
	var match *locationRec
	for _, loc := range lc.Records {
		if loc.Key == key {
			match = &loc
			break
		}
	}
	return match
}

func (lc *locationContext) isCourseReserve(key string) bool {
	match := false
	for _, loc := range lc.ReserveLocations {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isOnShelf(key string) bool {
	match := false
	for _, loc := range lc.OnShelf {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isNonCirculating(key string) bool {
	match := false
	for _, loc := range lc.NonCirculating {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isOnline(key string) bool {
	online := false
	for _, loc := range []string{"INTERNET", "NOTOREPDA"} {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			online = true
			break
		}
	}
	return online
}

func (lc *locationContext) isIvyStacks(key string) bool {
	return strings.TrimSpace(strings.ToUpper(key)) == "SC-IVY"
}

func (lc *locationContext) isMediumRare(key string) bool {
	return strings.TrimSpace(strings.ToUpper(key)) == "LOCKEDSTKS"
}

func (lc *locationContext) mediumRareMessage() string {
	return "This item does not circulate outside of library spaces. When you request this item from Ivy, it will be delivered to the Small Special Collections Library for you to use in the reading room only."
}

func (lc *locationContext) isUnavailable(key string) bool {
	online := false
	unavail := []string{"LOST", "UNKNOWN", "MISSING", "DISCARD", "WITHDRAWN", "BARRED", "BURSARED", "ORD-CANCLD", "HEREDOC"}
	for _, loc := range unavail {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			online = true
			break
		}
	}
	return online
}
