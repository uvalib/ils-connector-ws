package main

import (
	"encoding/json"
	"log"
	"slices"
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

	url := "/policy/library/simpleQuery?key=*&includeFields=key,policyNumber,description"
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
			Description: strings.TrimSpace(sl.Fields.Description),
		}
		lib.OnShelf = svc.Libraries.isOnShelf(sl.Key)
		lib.Circulating = !svc.Libraries.isNonCirculating(sl.Key)
		svc.Libraries.Records = append(svc.Libraries.Records, lib)
	}
	svc.Libraries.RefreshAt = time.Now().Add(24 * time.Hour)
	log.Printf("INFO: libraries refreshed")
}

func (lc *libraryContext) find(key string) *libraryRec {
	matchIdx := slices.IndexFunc(lc.Records, func(lib libraryRec) bool {
		return lib.Key == strings.TrimSpace(strings.ToUpper(key))
	})
	if matchIdx > -1 {
		return &lc.Records[matchIdx]
	}
	return nil
}

func (lc *libraryContext) lookupID(name string) string {
	matchIdx := slices.IndexFunc(lc.Records, func(lib libraryRec) bool {
		return lib.Description == strings.TrimSpace(name)
	})
	if matchIdx > -1 {
		return lc.Records[matchIdx].Key
	}
	return ""
}

func (lc *libraryContext) lookupPDALibrary(pdaLib string) string {
	pdaMap := map[string]string{
		"SH-PPDA": "SHANNON",
		"AL-PPDA": "ALD",
		"AS-PPDA": "ASTRO",
		"CH-PPDA": "CHEM",
		"CL-PPDA": "CLEM",
		"FA-PPDA": "FINE ARTS",
		"MA-PPDA": "MATH",
		"MU-PPDA": "MUSIC",
		"PH-PPDA": "PHYS",
		"SE-PPDA": "SCIENG",
	}
	return pdaMap[pdaLib]
}

func (lc *libraryContext) isNonCirculating(key string) bool {
	return slices.Contains(lc.NonCirculating, strings.TrimSpace(strings.ToUpper(key)))
}

func (lc *libraryContext) isOnShelf(key string) bool {
	return slices.Contains(lc.OnShelf, strings.TrimSpace(strings.ToUpper(key)))
}
