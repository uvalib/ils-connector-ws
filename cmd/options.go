package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/go-querystring/query"
)

type holdableItem struct {
	Barcode    string   `json:"barcode"`
	CallNumber string   `json:"callNumber"`
	Library    string   `json:"library"`
	Location   string   `json:"location"`
	Notice     string   `json:"notice,omitempty"`
	SCNotes    string   `json:"scNotes,omitempty"`  // only set based on solr doc for aeon items
	Requests   []string `json:"requests,omitempty"` // list of all types of options for requesting this item
	AeonURL    string   `json:"aeonURL,omitempty"`  // each aeon item is unique and will have its own holdableItem with a single request (aeon) and aeonURL
}

type requestOptions struct {
	Items            []*holdableItem `json:"items"`
	StreamingReserve bool            `json:"streamingVideoReserve,omitempty"`
	HSAScanURL       string          `json:"hsaScanURL,omitempty"`
	PDAURL           string          `json:"pdaURL,omitempty"`
}

func (ro *requestOptions) hasOptions() bool {
	if len(ro.Items) == 0 && ro.StreamingReserve == false && ro.HSAScanURL == "" && ro.PDAURL == "" {
		return false
	}
	return true
}

func createRequestOptions() *requestOptions {
	out := requestOptions{Items: make([]*holdableItem, 0)}
	return &out
}

func (svc *serviceContext) addSirsiRequestOptions(c *gin.Context, resp *availabilityResponse, items []availItem, marc sirsiBibData) {
	log.Printf("INFO: generate request options for %s with %d items", resp.TitleID, len(items))
	noScans := false
	var atoItem *availItem

	// check user profile and home location to see if scanning should be an option for this user
	v4Claims, _ := getVirgoClaims(c)
	ucaseProfile := strings.ToUpper(v4Claims.Profile)
	noScanProfiles := []string{"VABORROWER", "OTHERVAFAC", "ALUMNI", "RESEARCHER"}
	if slices.Contains(noScanProfiles, ucaseProfile) || v4Claims.HomeLibrary == "HEALTHSCI" {
		noScans = true
		log.Printf("INFO: user %s with profile [%s] and home library [%s] is not able to request scans",
			v4Claims.UserID, v4Claims.Profile, v4Claims.HomeLibrary)
	}

	for _, item := range items {
		// track available to order items for later use
		if item.CurrentLocation == "Available to Order" && atoItem == nil {
			atoItem = &item
		}

		// item must be available to be held/scanned
		if item.Unavailable {
			continue
		}

		// convert raw item data into a simplified holdable item and append special info for medium rare items
		holdableItem := item.toHoldableItem("")
		if svc.Locations.isMediumRare(item.HomeLocationID) {
			holdableItem.CallNumber += " (Ivy limited circulation)"
		}

		// First check to see if an item can be scanned since some non-circulating items are eligible for scanning
		itemJustAdded := false
		if item.IsVideo == false && noScans == false && item.LibraryID != "SPEC-COLL" {
			if slices.Contains([]string{"HISTCOL", "RARESHL", "RAREOVS", "RAREVLT"}, item.HomeLocationID) {
				log.Printf("INFO: %s with home location %s blocks this item from being scanned", item.Barcode, item.HomeLocationID)
				noScans = true
			} else {
				if ucaseProfile == "UNDERGRAD" && item.HomeLocationID != "BY-REQUEST" {
					// Per Daniel Stewart, undergraduate users can make scan requests for items located in a closed stack (BY-REQUEST).
					// Previous logic blocked all scan requests for undergraduate users
					log.Printf("INFO: undergraduate user %s cannot make scan requests for items in %s", v4Claims.UserID, item.HomeLocationID)
				} else {
					if holdableExists(holdableItem, item.Volume, resp.RequestOptions.Items) == false {
						itemJustAdded = true
						holdableItem.Requests = append(holdableItem.Requests, "scan")
						resp.RequestOptions.Items = append(resp.RequestOptions.Items, &holdableItem)
					}
				}
			}
		}

		// non circulating items are not holdable. This assumes (per original code)
		// that all users can request onshelf items. NOTE: this blocks SPEC-COLL items from the holdable list
		if svc.isNonCirculating(item) {
			continue
		}

		// If the scan logic above added the item to the items list, itemJustAdded will be true
		// which allows holds and videos to be added.
		if holdableExists(holdableItem, item.Volume, resp.RequestOptions.Items) == false || itemJustAdded {
			if itemJustAdded == false {
				// Only add the item if scan did not already add it
				resp.RequestOptions.Items = append(resp.RequestOptions.Items, &holdableItem)
			}
			holdableItem.Requests = append(holdableItem.Requests, "hold")
			if item.IsVideo {
				holdableItem.Requests = append(holdableItem.Requests, "videoReserve")
			}
		}
	}

	if atoItem != nil {
		log.Printf("INFO: add available to order option")
		url := fmt.Sprintf("%s/check/%s", svc.PDAURL, resp.TitleID)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Golang_ILS_Connector")
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.GetString("jwt")))
		_, err := svc.sendRequest("pda-ws", svc.HTTPClient, req)
		if err != nil {
			if err.StatusCode == 404 {
				resp.RequestOptions.PDAURL = svc.generatePDACreateURL(resp.TitleID, atoItem.Barcode, marc)
			} else {
				log.Printf("ERROR: pda check failed %d - %s", err.StatusCode, err.Message)
			}
		} else {
			// success here means the item has been orderd, but sirsi not yet updated
			log.Printf("INFO: %s is available for pda but has already been orderd", atoItem.Barcode)
			resp.RequestOptions.PDAURL = "ORDERED"
		}
	}
}

func (svc *serviceContext) addStreamingVideoOption(solrDoc *solrDocument, avail *availabilityResponse) {
	if solrDoc.Pool[0] == "video" && (slices.Contains(solrDoc.Location, "Internet materials") || slices.Contains(solrDoc.Source, "Avalon")) {
		log.Printf("INFO: add streaming video reserve option")
		avail.RequestOptions.StreamingReserve = true
	}
}

func (svc *serviceContext) addHSLScanOption(solrDoc *solrDocument, avail *availabilityResponse) {
	log.Printf("INFO: add scan option for hsl user")
	avail.RequestOptions.HSAScanURL = openURLQuery(svc.HSILLiadURL, solrDoc)
}

func (svc *serviceContext) addAeonRequestOptions(result *availabilityResponse, solrDoc *solrDocument, availItems []availItem) {
	log.Printf("INFO: add aeon request options")

	if !(slices.Contains(solrDoc.Library, "Special Collections")) {
		log.Printf("INFO: item %s library is not special collections; nothing to do", result.TitleID)
		return
	}

	for _, item := range availItems {
		if item.LibraryID != "SPEC-COLL" {
			continue
		}
		notes := ""
		if len(item.SCLocation) > 0 {
			notes = item.SCLocation
		} else if len(solrDoc.LocalNotes) > 0 {
			// drop name
			prefix1 := regexp.MustCompile(`^\s*SPECIAL\s+COLLECTIONS:\s+`)
			//shorten SC name
			prefix2 := regexp.MustCompile(`^\s*Harrison Small Special Collections,`)

			for _, note := range solrDoc.LocalNotes {
				note = prefix1.ReplaceAllString(note, "")
				note = prefix2.ReplaceAllString(note, "H. Small,")
				notes += (strings.TrimSpace(note) + ";\n")
			}
		} else {
			notes = "(no location notes)"
		}

		if len(notes) > 700 {
			notes = notes[:700]
		}

		// Each aeon item is unique and will have its own aron request URL
		aeonItem := item.toHoldableItem(notes)
		aeonItem.Requests = append(aeonItem.Requests, "aeon")
		aeonURL, urlErr := createAeonURL(aeonItem, solrDoc)
		if urlErr != nil {
			log.Printf("ERROR: unable to generate aeon url for barcode %s", aeonItem.Barcode)
		} else {
			aeonItem.AeonURL = aeonURL
		}
		result.RequestOptions.Items = append(result.RequestOptions.Items, &aeonItem)
	}
}

func createAeonURL(item holdableItem, doc *solrDocument) (string, error) {
	type aeonRequest struct {
		Action      int    `url:"Action"`
		Form        int    `url:"Form"`
		Value       string `url:"Value"` // either GenericRequestManuscript or GenericRequestMonograph
		DocID       string `url:"ReferenceNumber"`
		Title       string `url:"ItemTitle"`
		Author      string `url:"ItemAuthor"`
		Date        string `url:"ItemDate"`
		ISxN        string `url:"ItemISxN"`
		CallNumber  string `url:"CallNumber"`
		Barcode     string `url:"ItemNumber"`
		Place       string `url:"ItemPlace"`
		Publisher   string `url:"ItemPublisher"`
		Edition     string `url:"ItemEdition"`
		Issue       string `url:"ItemIssuesue"`
		Volume      string `url:"ItemVolume"`
		Copy        string `url:"ItemInfo2"`
		Location    string `url:"Location"`
		Description string `url:"ItemInfo1"`
		Notes       string `url:"Notes"`
	}

	// Decide monograph or manuscript
	formValue := "GenericRequestMonograph"

	if slices.Contains(doc.WorkTypes, "manuscript") ||
		slices.Contains(doc.Medium, "manuscript") ||
		slices.Contains(doc.Format, "manuscript") ||
		slices.Contains(doc.WorkTypes, "collection") {
		formValue = "GenericRequestManuscript"
	}

	desc := strings.Join(doc.Description, "; ")
	if len(desc) > 100 {
		desc = desc[:100]
	}

	req := aeonRequest{
		Action:      10,
		Form:        20,
		Value:       formValue,
		DocID:       doc.ID,
		Title:       strings.Join(doc.Title, "; "),
		Date:        doc.PublicationDate,
		ISxN:        strings.Join(append(doc.ISBN, doc.ISSN...), ";"),
		Place:       strings.Join(doc.PublishedLocation, "; "),
		Publisher:   strings.Join(doc.PublisherName, "; "),
		Edition:     doc.Edition,
		Issue:       doc.Issue,
		Volume:      item.CallNumber, // TODO this seems wrong, bit it is the way the system originally worked. doc.Volume is available and was set here, but overridden in client
		Copy:        doc.Copy,
		Description: desc,
		CallNumber:  item.CallNumber,
		Barcode:     item.Barcode,
		Notes:       item.SCNotes,
		Location:    item.Location,
	}
	if len(doc.Author) == 1 {
		req.Author = doc.Author[0]
	} else if len(doc.Author) > 1 {
		req.Author = fmt.Sprintf("%s; ...", doc.Author[0])
	}

	query, err := query.Values(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://virginia.aeon.atlas-sys.com/logon?%s", query.Encode())
	return url, nil
}

func holdableExists(tgtItem holdableItem, volume string, holdableItems []*holdableItem) bool {
	exist := slices.ContainsFunc(holdableItems, func(hi *holdableItem) bool {
		return strings.EqualFold(hi.CallNumber, tgtItem.CallNumber)
	})
	if exist == false {
		// NOTES: even if call is unique, the item may be considered the same if it does not
		// have any volume info. Ex: u5841451 is a video with 2 unique callnumns:
		// VIDEO .DVD19571 and KLAUS DVD #1224. Since neiter is a different volume they are considered
		// the same item from a request persective. Only add the first instance of such items.
		return volume == "" && len(holdableItems) > 0
	}
	return exist
}
