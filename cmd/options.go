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
	Barcode    string `json:"barcode"`
	CallNumber string `json:"call_number"`
	Library    string `json:"library"`
	Location   string `json:"location"`
	Notice     string `json:"notice"`
	SCNotes    string `json:"sc_notes,omitempty"` // only set based on solr doc for aeon items
}

type requestOption struct {
	Type             string         `json:"type"`
	SignInRequired   bool           `json:"sign_in_required"`
	StreamingReserve bool           `json:"streaming_reserve"`
	ItemOptions      []holdableItem `json:"item_options"`
	CreateURL        string         `json:"create_url"`
}

func (svc *serviceContext) generateRequestOptions(c *gin.Context, titleID string, items []availItem, marc sirsiBibData) []requestOption {
	log.Printf("INFO: generate request options for %s", titleID)
	out := make([]requestOption, 0)
	holdableItems := make([]holdableItem, 0)
	videoItemOpts := make([]holdableItem, 0)
	scanItemOpts := make([]holdableItem, 0)
	var atoItem *availItem

	for _, item := range items {
		// track available to order items for later use
		if item.CurrentLocation == "Available to Order" && atoItem == nil {
			atoItem = &item
		}

		// unavailable or non circulating items are not holdable. This assumes (per original code)
		// that al users can request onshelf items. NOTE: this blocks SPEC-COLL items from the holdable list
		if item.Unavailable || svc.isNonCirculating(item) {
			continue
		}

		holdableItem := item.toHoldableItem("")
		if svc.Locations.isMediumRare(item.HomeLocationID) {
			holdableItem.CallNumber += " (Ivy limited circulation)"
		}
		if holdableExists(holdableItem, item.Volume, holdableItems) == false {
			holdableItems = append(holdableItems, holdableItem)

			if item.IsVideo {
				videoItemOpts = append(videoItemOpts, holdableItem)
			} else {
				v4Claims, _ := getVirgoClaims(c)
				ucaseProfile := strings.ToUpper(v4Claims.Profile)
				noScanProfiles := []string{"VABORROWER", "OTHERVAFAC", "ALUMNI", "RESEARCHER", "UNDERGRAD"}
				if listContains(noScanProfiles, ucaseProfile) == false && v4Claims.HomeLibrary != "HEALTHSCI" {
					scanItemOpts = append(scanItemOpts, holdableItem)
				} else {
					log.Printf("INFO: user %s with profile [%s] and home library [%s] is not able to request scans",
						v4Claims.UserID, v4Claims.Profile, v4Claims.HomeLibrary)
				}
			}
		}
	}

	if len(holdableItems) > 0 {
		log.Printf("INFO: add hold options for %s", titleID)
		out = append(out, requestOption{Type: "hold", SignInRequired: true,
			ItemOptions: holdableItems,
		})
	}

	if len(videoItemOpts) > 0 {
		log.Printf("INFO: add video reserve options for %s", titleID)
		out = append(out, requestOption{Type: "videoReserve", SignInRequired: true,
			ItemOptions: videoItemOpts,
		})
	}

	if len(scanItemOpts) > 0 {
		log.Printf("INFO: add scan options for %s", titleID)
		out = append(out, requestOption{Type: "scan", SignInRequired: true,
			ItemOptions: scanItemOpts,
		})
	}

	if atoItem != nil {
		log.Printf("INFO: add available to order option")
		url := fmt.Sprintf("%s/check/%s", svc.PDAURL, titleID)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Golang_ILS_Connector")
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.GetString("jwt")))
		_, err := svc.sendRequest("pda-ws", svc.HTTPClient, req)
		if err != nil {
			if err.StatusCode == 404 {
				out = append(out, requestOption{Type: "pda", SignInRequired: true,
					ItemOptions: make([]holdableItem, 0),
					CreateURL:   svc.generatePDACreateURL(titleID, atoItem.Barcode, marc),
				})
			} else {
				log.Printf("ERROR: pda check failed %d - %s", err.StatusCode, err.Message)
			}
		} else {
			// success here means the item has been orderd, but sirsi not yet updated
			out = append(out, requestOption{Type: "pda", SignInRequired: true,
				ItemOptions: make([]holdableItem, 0),
			})
		}
	}

	return out
}

func (svc *serviceContext) addStreamingVideoOption(solrDoc *solrDocument, avail *availabilityResponse) {
	if solrDoc.Pool[0] == "video" && (listContains(solrDoc.Location, "Internet materials") ||
		listContains(solrDoc.Source, "Avalon")) {

		log.Printf("Adding streaming video reserve option")
		option := requestOption{
			Type:             "videoReserve",
			SignInRequired:   true,
			StreamingReserve: true,
			ItemOptions:      make([]holdableItem, 0),
		}
		avail.RequestOptions = append(avail.RequestOptions, option)
	}
}

func (svc *serviceContext) updateHSLScanOptions(solrDoc *solrDocument, avail *availabilityResponse) {
	log.Printf("INFO: update scan options for hsl user")

	avail.RequestOptions = slices.DeleteFunc(avail.RequestOptions, func(opt requestOption) bool {
		return opt.Type == "scan"
	})

	hsScan := requestOption{
		Type:           "directLink",
		SignInRequired: false,
		CreateURL:      openURLQuery(svc.HSILLiadURL, solrDoc),
		ItemOptions:    make([]holdableItem, 0),
	}
	avail.RequestOptions = append(avail.RequestOptions, hsScan)
}

func (svc *serviceContext) addAeonRequestOptions(result *availabilityResponse, solrDoc *solrDocument, availItems []availItem) {
	log.Printf("INFO: add aeon request options")

	if !(listContains(solrDoc.Library, "Special Collections")) {
		log.Printf("INFO: item %s library is not special collections; nothing to do", result.TitleID)
		return
	}

	aeonOption := requestOption{
		Type:           "aeon",
		SignInRequired: false,
		CreateURL:      createAeonURL(solrDoc),
		ItemOptions:    createAeonItemOptions(solrDoc, availItems),
	}
	result.RequestOptions = append(result.RequestOptions, aeonOption)
}

func createAeonItemOptions(solrDoc *solrDocument, availItems []availItem) []holdableItem {
	options := []holdableItem{}
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

		options = append(options, item.toHoldableItem(notes))
	}

	return options
}

func createAeonURL(doc *solrDocument) string {
	type aeonRequest struct {
		Action      int    `url:"Action"`
		Form        int    `url:"Form"`
		Value       string `url:"Value"` // either GenericRequestManuscript or GenericRequestMonograph
		DocID       string `url:"ReferenceNumber"`
		Title       string `url:"ItemTitle" default:"(NONE)"`
		Author      string `url:"ItemAuthor"`
		Date        string `url:"ItemDate"`
		ISxN        string `url:"ItemISxN"`
		CallNumber  string `url:"CallNumber" default:"(NONE)"`
		Barcode     string `url:"ItemNumber"`
		Place       string `url:"ItemPlace"`
		Publisher   string `url:"ItemPublisher"`
		Edition     string `url:"ItemEdition"`
		Issue       string `url:"ItemIssuesue"`
		Volume      string `url:"ItemVolume"` // unless manuscript
		Copy        string `url:"ItemInfo2"`
		Location    string `url:"Location"`
		Description string `url:"ItemInfo1"`
		Notes       string `url:"Notes"`
		Tags        string `url:"ResearcherTags,omitempty"`
		UserNote    string `url:"SpecialRequest"`
	}

	// Decide monograph or manuscript
	formValue := "GenericRequestMonograph"

	if listContains(doc.WorkTypes, "manuscript") ||
		listContains(doc.Medium, "manuscript") ||
		listContains(doc.Format, "manuscript") ||
		listContains(doc.WorkTypes, "collection") {
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
		Volume:      doc.Volume,
		Copy:        doc.Copy,
		Description: desc,
	}
	if len(doc.Author) == 1 {
		req.Author = doc.Author[0]
	} else if len(doc.Author) > 1 {
		req.Author = fmt.Sprintf("%s; ...", doc.Author[0])
	}

	// Notes, Bacode, CallNumber, UserNotes need to be added by client for the specific item!

	query, _ := query.Values(req)

	url := fmt.Sprintf("https://virginia.aeon.atlas-sys.com/logon?%s", query.Encode())
	return url
}

func holdableExists(tgtItem holdableItem, volume string, holdableItems []holdableItem) bool {
	exist := false
	for _, hi := range holdableItems {
		if strings.ToUpper(hi.CallNumber) == strings.ToUpper(tgtItem.CallNumber) {
			exist = true
			break
		}
	}
	if exist == false {
		// NOTES: even if call is unique, the item may be considered the same if it does not
		// have any volume info. Ex: u5841451 is a video with 2 unique callnumns:
		// VIDEO .DVD19571 and KLAUS DVD #1224. Since neiter is a different volume they are considered
		// the same item from a request persective. Only add the first instance of such items.
		return volume == "" && len(holdableItems) > 0
	}
	return exist
}
