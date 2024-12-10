package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type locationContext struct {
	Records        []locationRec
	NonCirculating struct {
		Libraries []string
		Locations []string
	}
	OnShelf struct {
		Libraries []string
		Locations []string
	}
	RefreshAt time.Time
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
	ID             int    `json:"id"`
	Key            string `json:"key"`
	Description    string `json:"description"`
	OnShelf        bool   `json:"onShelf"`
	NonCirculating bool   `json:"nonCirculating"`
	Online         bool   `json:"online"`
	Unavailable    bool   `json:"unavailable"`
	Shadowed       bool   `json:"shadowed"`
}

func (svc *serviceContext) refreshLocations() {
	if len(svc.Locations.NonCirculating.Locations) == 0 {
		log.Printf("INFO: load non-circulating data")
		svc.Locations.NonCirculating.Libraries = loadLocationData("./data/noncirc-lib.txt")
		svc.Locations.NonCirculating.Locations = loadLocationData("./data/noncirc-loc.txt")
	}
	if len(svc.Locations.OnShelf.Locations) == 0 {
		log.Printf("INFO: load on shelf data")
		svc.Locations.OnShelf.Libraries = loadLocationData("./data/onshelf-lib.txt")
		svc.Locations.OnShelf.Locations = loadLocationData("./data/onshelf-loc.txt")
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
		return locs, fmt.Errorf(sirsiErr.string())
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
		loc.NonCirculating = svc.Locations.isNonCirculatingLocation(sl.Key)
		loc.Online = svc.Locations.isOnlineLocation(sl.Key)
		loc.Unavailable = svc.Locations.isUnavailableLocation(sl.Key)
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
	for _, loc := range lc.OnShelf.Locations {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isOnShelfLibrary(key string) bool {
	match := false
	for _, loc := range lc.OnShelf.Libraries {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isNonCirculatingLocation(key string) bool {
	match := false
	for _, loc := range lc.NonCirculating.Locations {
		if loc == strings.TrimSpace(strings.ToUpper(key)) {
			match = true
		}
	}
	return match
}

func (lc *locationContext) isNonCirculatingLibrary(key string) bool {
	match := false
	for _, loc := range lc.NonCirculating.Libraries {
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

func loadLocationData(filename string) []string {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("ERROR: unable to load %s: %s", filename, err.Error())
		return make([]string, 0)
	}
	return strings.Split(string(bytes), "\n")
}
