package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

type solrDocument struct {
	AnonAvailability  []string `json:"anon_availability_a,omitempty"`
	Author            []string `json:"author_a,omitempty"`
	Barcode           []string `json:"barcode_a,omitempty"`
	CallNumber        []string `json:"call_number_a,omitempty"`
	Copy              string   `json:"-"`
	Description       []string `json:"description_a,omitempty"`
	Edition           string   `json:"-"`
	HathiETAS         []string `json:"hathi_etas_f,omitempty"`
	Issue             string   `json:"-"`
	Format            []string `json:"format_a,omitempty"`
	ID                string   `json:"id,omitempty"`
	ISBN              []string `json:"isbn_a,omitempty"`
	ISSN              []string `json:"issn_a,omitempty"`
	Library           []string `json:"library_a,omitempty"`
	Location          []string `json:"location2_a,omitempty"`
	LocalNotes        []string `json:"local_notes_a,omitempty"`
	Medium            []string `json:"medium_a,omitempty"`
	Pool              []string `json:"pool_f,omitempty"`
	PublicationDate   string   `json:"published_date,omitempty"`
	PublishedLocation []string `json:"published_location_a,omitempty"`
	PublisherName     []string `json:"publisher_name_a,omitempty"`
	SCAvailability    string   `json:"sc_availability_large_single,omitempty"`
	Source            []string `json:"source_a,omitempty"`
	Title             []string `json:"title_a,omitempty"`
	URL               []string `json:"url_a,omitempty"`
	Volume            string   `json:"-"`
	WorkTypes         []string `json:"workType_a,omitempty"`
}

func (doc solrDocument) fieldList() string {
	// Use reflection to pull json tags contained in the SolrDocument struct
	// to create the field list for the solr query
	rv := reflect.ValueOf(doc)
	t := rv.Type()
	matcher := regexp.MustCompile(`(\w+),`)
	var tags []string
	for i := 0; i < t.NumField(); i++ {
		value := t.Field(i).Tag.Get("json")
		matches := matcher.FindAllStringSubmatch(value, -1)
		if len(matches) > 0 && len(matches[0]) > 0 {
			tags = append(tags, matches[0][1])
		}
	}
	fields := url.QueryEscape(strings.Join(tags, ","))
	return fields
}

type solrResponse struct {
	Response struct {
		Docs     []solrDocument `json:"docs,omitempty"`
		NumFound int            `json:"numFound,omitempty"`
	} `json:"response,omitempty"`
}

func (svc *serviceContext) getSolrDoc(catKey string) (*solrDocument, error) {
	log.Printf("INFO: get solr doc for %s", catKey)
	fields := solrDocument{}.fieldList()
	solrPath := fmt.Sprintf(`select?fl=%s,&q=id%%3A%s`, fields, catKey)

	respBytes, solrErr := svc.solrGet(solrPath)
	if solrErr != nil {
		return nil, fmt.Errorf("get solr doc failed: %s", solrErr.string())
	}
	var solrResp solrResponse
	if err := json.Unmarshal(respBytes, &solrResp); err != nil {
		return nil, fmt.Errorf("unable to parse solr response: %s", err.Error())
	}
	if solrResp.Response.NumFound == 0 {
		return nil, fmt.Errorf("no solr document found for %s", catKey)
	}
	if solrResp.Response.NumFound > 1 {
		log.Printf("WARNING: more than one record found for the id: %s", catKey)
	}
	solrDoc := solrResp.Response.Docs[0]
	return &solrDoc, nil
}

func (svc *serviceContext) solrGet(query string) ([]byte, *requestError) {
	url := fmt.Sprintf("%s/%s/%s", svc.Solr.URL, svc.Solr.Core, query)
	req, _ := http.NewRequest("GET", url, nil)
	return svc.sendRequest("solr", svc.HTTPClient, req)
}
