package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

type holdableItem struct {
	Barcode    string `json:"barcode"`
	Label      string `json:"label"`
	Library    string `json:"library"`
	Location   string `json:"location"`
	LocationID string `json:"location_id"`
	IsVideo    bool   `json:"is_video"`
	Notice     string `json:"notice"`
	Volume     string `json:"-"`
}

type requestOption struct {
	Type           string         `json:"type"`
	SignInRequired bool           `json:"sign_in_required"`
	ButtonLabel    string         `json:"button_label"`
	Description    string         `json:"description"`
	ItemOptions    []holdableItem `json:"item_options"`
	CreateURL      string         `json:"create_url"`
}

func (svc *serviceContext) generateRequestOptions(userJWT string, titleID string, items []availItem, marc sirsiBibData) []requestOption {
	out := make([]requestOption, 0)
	holdableItems := make([]holdableItem, 0)
	var atoItem availItem

	for _, item := range items {
		// track available to order items for later use
		if item.CurrentLocation == "Available to Order" && atoItem.CurrentLocationID == "" {
			atoItem = item
		}

		// unavailable or non circulating items are not holdable. This assumes (per original code)
		// that al users can request onshelf items
		if item.Unavailable || item.NonCirculating {
			continue
		}

		holdableItem := item.toHoldableItem()
		if svc.Locations.isMediumRare(item.HomeLocationID) {
			holdableItem.Label += " (Ivy limited circulation)"
		}
		if holdableExists(holdableItem, holdableItems) == false {
			holdableItems = append(holdableItems, holdableItem)
		}
	}

	if len(holdableItems) > 0 {
		log.Printf("INFO: add hold options for %s", titleID)
		out = append(out, requestOption{Type: "hold", SignInRequired: true,
			ButtonLabel: "Request item",
			Description: "Request an unavailable item or request delivery.",
			ItemOptions: holdableItems,
		})

		nonVideo := make([]holdableItem, 0)
		videos := make([]holdableItem, 0)
		for _, item := range holdableItems {
			if item.IsVideo == false {
				nonVideo = append(nonVideo, item)
			} else {
				videos = append(videos, item)
			}
		}
		if len(nonVideo) > 0 {
			log.Printf("INFO: add scan options for %s", titleID)
			out = append(out, requestOption{Type: "scan", SignInRequired: true,
				ButtonLabel: "Request a scan",
				Description: "Select a portion of this item to be scanned.",
				ItemOptions: nonVideo,
			})
		}

		if len(videos) > 0 {
			log.Printf("INFO: add video reserve options for %s", titleID)
			out = append(out, requestOption{Type: "videoReserve", SignInRequired: true,
				ButtonLabel: "Video reserve request",
				Description: "Request a video reserve for streaming.",
				ItemOptions: videos,
			})
		}
	}

	if atoItem.CurrentLocationID != "" {
		log.Printf("INFO: add available to order option")
		url := fmt.Sprintf("%s/check/%s", svc.PDAURL, titleID)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Golang_ILS_Connector")
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", userJWT))
		_, err := svc.sendRequest("pda-ws", svc.HTTPClient, req)
		if err != nil {
			if err.StatusCode == 404 {
				out = append(out, requestOption{Type: "pda", SignInRequired: true,
					ButtonLabel: "Order this Item",
					Description: `<div class="pda-about">Learn more about <a href="https://library.virginia.edu/about-available-to-order-items" aria-label="Available to Order" style="text-decoration: underline;" target="_blank" title="Available to Order (Opens in a new window.)" class="piwik_link">Available to Order</a> items.</div>`,
					ItemOptions: make([]holdableItem, 0),
					CreateURL:   svc.generatePDACreateURL(titleID, atoItem.Barcode, marc),
				})
			} else {
				log.Printf("ERROR: pda check failed %d - %s", err.StatusCode, err.Message)
			}
		} else {
			// success here means the item has been orderd, but sirsi not yet updated
			out = append(out, requestOption{Type: "pda", SignInRequired: true,
				Description: `<div class="pda-about">This item is now on order. Learn more about <a href="https://library.virginia.edu/about-available-to-order-items" aria-label="Available to Order" style="text-decoration: underline;" target="_blank" title="Available to Order (Opens in a new window.)" class="piwik_link">Available to Order</a> items.</div>`,
				ItemOptions: make([]holdableItem, 0),
			})
		}
	}

	return out
}

func holdableExists(tgtItem holdableItem, holdableItems []holdableItem) bool {
	exist := false
	for _, hi := range holdableItems {
		if strings.ToUpper(hi.Label) == strings.ToUpper(tgtItem.Label) || hi.Barcode == tgtItem.Barcode {
			exist = true
			break
		}
	}
	if exist == false {
		// NOTES: even if call / barcode is unique, the item may be considered the same if it does not
		// have any volume info. Ex: u5841451 is a video with 2 unique callnumns:
		// VIDEO .DVD19571 and KLAUS DVD #1224. Since neiter is a different volume they are considered
		// the same item from a request persective. Only add the first instance of such items.
		return tgtItem.Volume == "" && len(holdableItems) > 0
	}
	return exist
}
