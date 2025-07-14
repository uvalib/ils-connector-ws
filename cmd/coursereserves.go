package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
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

type searchHit struct {
	ID          string   `json:"id"`
	Title       []string `json:"title_a"`
	Author      []string `json:"work_primary_author_a"`
	CallNumber  []string `json:"call_number_a"`
	ReserveInfo []string `json:"reserve_id_course_name_a"`
}

type searchReservesResponse struct {
	Response struct {
		Docs     []searchHit `json:"docs,omitempty"`
		NumFound int         `json:"numFound,omitempty"`
	} `json:"response,omitempty"`
}

type validateRespRec struct {
	ID      string `json:"id"`
	Reserve bool   `json:"reserve"`
	IsVideo bool   `json:"is_video"`
}

type reserveItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	CallNumber string `json:"callNumber"`
}

type courseItems struct {
	CourseName string        `json:"courseName"`
	CourseID   string        `json:"courseID"`
	Items      []reserveItem `json:"items"`
}

type instructorSearchResponse struct {
	InstructorName string         `json:"instructorName"`
	Courses        []*courseItems `json:"courses"`
}

type instructorItems struct {
	InstructorName string        `json:"instructorName"`
	Items          []reserveItem `json:"items"`
}

type courseSearchResponse struct {
	CourseName  string             `json:"courseName"`
	CourseID    string             `json:"courseID"`
	Instructors []*instructorItems `json:"instructors"`
}

type requestParams struct {
	OnBehalfOf      string `json:"onBehalfOf"`
	InstructorName  string `json:"instructorName"`
	InstructorEmail string `json:"instructorEmail"`
	Name            string `json:"name"`
	Email           string `json:"email"`
	Course          string `json:"course"`
	Semester        string `json:"semester"`
	Library         string `json:"library"`
	Period          string `json:"period"`
	LMS             string `json:"lms"`
	OtherLMS        string `json:"otherLMS"`
}

type availabilityInfo struct {
	Library      string `json:"library"`
	Location     string `json:"location"`
	Availability string `json:"availability"`
	CallNumber   string `json:"callNumber"`
}

type requestItem struct {
	Pool             string             `json:"pool"`
	IsVideo          bool               `json:"isVideo"`
	CatalogKey       string             `json:"catalogKey"`
	CallNumber       []string           `json:"callNumber"`
	Title            string             `json:"title"`
	Author           string             `json:"author"`
	Period           string             `json:"period"`
	Notes            string             `json:"notes"`
	AudioLanguage    string             `json:"audioLanguage"`
	Subtitles        string             `json:"subtitles"`
	SubtitleLanguage string             `json:"subtitleLanguage"`
	VirgoURL         string             `json:"-"`
	Availability     []availabilityInfo `json:"-"`
}

type reserveRequest struct {
	VirgoURL string
	UserID   string         `json:"userID"`
	Request  requestParams  `json:"request"`
	Items    []requestItem  `json:"items"` // these are the items sent from the client
	Video    []*requestItem `json:"-"`     // populated during processing from Items, includes avail
	NonVideo []*requestItem `json:"-"`     // populated during processing from Items, includes avail
	MaxAvail int            `json:"-"`
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
	log.Printf("INFO: validate course reserves %v", req.Items)

	idMap := make(map[string]string)
	var bits []string
	for _, key := range req.Items {
		cleanKey := cleanCatKey(key)
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
	for cleanKey, origID := range idMap {
		respRec := validateRespRec{ID: origID, IsVideo: false, Reserve: false}

		// find the item in the sirsi response if possible
		var rec *sirsiSearchRec
		for _, item := range resp.Result {
			if item.Key == cleanKey {
				rec = &item
			}
		}
		if rec == nil {
			log.Printf("INFO: %s not found in sirsi", origID)
		} else {
			for _, cl := range rec.Fields.CallList {
				for _, item := range cl.Fields.ItemList {
					itemType := item.Fields.ItemType.Key
					respRec.IsVideo = isVideo(itemType)
					if respRec.IsVideo == true {
						log.Printf("INFO: %s is video (%s) and may be a candidate for reserve", rec.Key, itemType)
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
				}
				if respRec.Reserve == true {
					break
				}
			}
		}

		// for rejected or non-video items, look them up in solr and determine if
		// they are actually a video/streaming video and flag correctly
		// (sirsi have enout info to determine this completely)
		if respRec.IsVideo == false || respRec.Reserve == false {
			log.Printf("INFO: sirsi data has video %t and reserve %t; check solr doc", respRec.IsVideo, respRec.Reserve)
			solrDoc, err := svc.getSolrDoc(respRec.ID)
			if err != nil {
				log.Printf("ERROR: unable to get solr doc for %s: %s", respRec.ID, err.Error())
			} else {
				if (solrDoc.Pool[0] == "video" && listContains(solrDoc.Location, "Internet materials")) || listContains(solrDoc.Source, "Avalon") {
					log.Printf("INFO: per solr document, %s is a video", respRec.ID)
					respRec.IsVideo = true
					respRec.Reserve = true
				}
			}
		}
		out = append(out, respRec)
	}

	c.JSON(http.StatusOK, out)
}

func (svc *serviceContext) createCourseReserves(c *gin.Context) {
	var reserveReq reserveRequest
	err := c.ShouldBindJSON(&reserveReq)
	if err != nil {
		log.Printf("ERROR: Unable to parse request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	reserveReq.VirgoURL = svc.VirgoURL
	reserveReq.MaxAvail = -1
	reserveReq.Video = make([]*requestItem, 0)
	reserveReq.NonVideo = make([]*requestItem, 0)
	v4Claims, _ := getVirgoClaims(c)
	log.Printf("INFO: %s requests creation of course reserves", v4Claims.UserID)

	// Iterate thru all of the requested items, pull availability and stuff it into
	// an array based on type. Separate emails will go out for video / non-video
	for _, item := range reserveReq.Items {
		item.VirgoURL = fmt.Sprintf("%s/sources/%s/items/%s", svc.VirgoURL, item.Pool, item.CatalogKey)
		avail, err := svc.getCourseReserveItemAvailability(item.CatalogKey)
		if err != nil {
			log.Printf("WARN: %s, ", err.Error())
		}
		item.Availability = avail
		if len(item.Availability) > reserveReq.MaxAvail {
			reserveReq.MaxAvail = len(item.Availability)
		}
		if item.IsVideo {
			log.Printf("INFO: %s : %s is a video", item.CatalogKey, item.Title)
			reserveReq.Video = append(reserveReq.Video, &item)
		} else {
			log.Printf("INFO: %s : %s is not a video", item.CatalogKey, item.Title)
			reserveReq.NonVideo = append(reserveReq.NonVideo, &item)
		}
	}

	funcs := template.FuncMap{"add": func(x, y int) int {
		return x + y
	}}

	templates := [2]string{"reserves.txt", "reserves_video.txt"}
	for _, templateFile := range templates {
		if templateFile == "reserves.txt" && len(reserveReq.NonVideo) == 0 {
			continue
		}
		if templateFile == "reserves_video.txt" && len(reserveReq.Video) == 0 {
			continue
		}
		var renderedEmail bytes.Buffer
		tpl := template.Must(template.New(templateFile).Funcs(funcs).ParseFiles(fmt.Sprintf("templates/%s", templateFile)))
		err = tpl.Execute(&renderedEmail, reserveReq)
		if err != nil {
			log.Printf("ERROR: Unable to render %s: %s", templateFile, err.Error())
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		log.Printf("Generate SMTP message for %s", templateFile)
		// NOTES for recipient: For any reserve library location other than Law, the email should be sent to
		// svc.CourseReserveEmail with the from address of the patron submitting the request.
		// For Law it should send the email to svc.LawReserveEmail AND the patron
		to := []string{}
		cc := ""
		from := svc.SMTP.Sender
		subjectName := reserveReq.Request.Name
		if reserveReq.Request.Library == "law" {
			log.Printf("The reserve library is law. Send request to law %s and requestor %s from sender %s",
				svc.LawReserveEmail, reserveReq.Request.Email, svc.SMTP.Sender)
			to = append(to, svc.LawReserveEmail)
			to = append(to, reserveReq.Request.Email)
			if reserveReq.Request.InstructorEmail != "" {
				to = append(to, reserveReq.Request.InstructorEmail)
			}
		} else {
			log.Printf("The reserve library is not law.")
			to = append(to, svc.CourseReserveEmail)
			if reserveReq.Request.InstructorEmail != "" {
				from = reserveReq.Request.InstructorEmail
				cc = reserveReq.Request.Email
				subjectName = reserveReq.Request.InstructorName
			} else {
				from = reserveReq.Request.Email
			}
		}

		subject := fmt.Sprintf("%s - %s: %s", reserveReq.Request.Semester, subjectName, reserveReq.Request.Course)
		eRequest := emailRequest{Subject: subject, To: to, CC: cc, From: from, Body: renderedEmail.String()}
		sendErr := svc.sendEmail(&eRequest)
		if sendErr != nil {
			log.Printf("ERROR: Unable to send reserve email: %s", sendErr.Error())
			c.String(http.StatusInternalServerError, sendErr.Error())
			return
		}
	}
	c.String(http.StatusOK, "Reserve emails sent")
}

func (svc *serviceContext) searchCourseReserves(c *gin.Context) {
	searchType := c.Query("type")
	if searchType != "instructor_name" && searchType != "course_id" {
		log.Printf("ERROR: invalid course reserves search type: %s", searchType)
		c.String(http.StatusBadRequest, fmt.Sprintf("%s is not a valid search type", searchType))
		return
	}
	rawQueryStr := c.Query("query")
	queryStr := rawQueryStr
	if strings.Contains(queryStr, "*") == false {
		queryStr += "*"
	}

	log.Printf("INFO: search [%s] course reserves for [%s]", searchType, queryStr)

	fl := url.QueryEscape("id,reserve_id_course_name_a,title_a,work_primary_author_a,call_number_a")
	queryParam := "reserve_id_a"
	if searchType == "instructor_name" {
		queryParam = "reserve_instructor_tl"
		queryStr = url.PathEscape(queryStr)
		// working format example: q=reserve_instructor_tl:beardsley%2C%20s*
	} else {
		// course IDs are in all upper case. force query to match
		queryStr = strings.ToUpper(queryStr)
		if strings.Contains(queryStr, " ") {
			queryStr = strings.ReplaceAll(queryStr, " ", "\\ ")
			queryStr = url.QueryEscape(queryStr)
		}
	}

	queryParam = fmt.Sprintf("%s:%s", queryParam, queryStr)
	solrURL := fmt.Sprintf("select?fl=%s&q=%s&rows=5000", fl, queryParam)

	respBytes, solrErr := svc.solrGet(solrURL)
	if solrErr != nil {
		log.Printf("ERROR: solr course reserves search failed: %s", solrErr.Message)
		// c.String(solrErr
		return
	}
	var solrResp searchReservesResponse
	if err := json.Unmarshal(respBytes, &solrResp); err != nil {
		log.Printf("ERROR: unable to parse solr response: %s.", err.Error())
	}
	log.Printf("INFO: found [%d] matches", solrResp.Response.NumFound)

	if searchType == "instructor_name" {
		reserves := extractInstructorReserves(rawQueryStr, solrResp.Response.Docs)
		c.JSON(http.StatusOK, reserves)
		return
	}

	reserves := extractCourseReserves(rawQueryStr, solrResp.Response.Docs)
	c.JSON(http.StatusOK, reserves)
}

func (svc *serviceContext) getCourseReserveItemAvailability(catKey string) ([]availabilityInfo, error) {
	log.Printf("INFO: check if item %s is available for course reserve", catKey)
	bibResp, sirsiErr := svc.getSirsiItem(catKey)
	if sirsiErr != nil {
		return nil, fmt.Errorf("get sirsi item availability failed %s", sirsiErr.string())
	}
	availItems := svc.parseItemsFromSirsi(bibResp)

	out := make([]availabilityInfo, 0)
	for _, availItem := range availItems {
		avail := availabilityInfo{Library: availItem.Library, Location: availItem.CurrentLocation, CallNumber: availItem.CallNumber}
		out = append(out, avail)
	}
	return out, nil
}

func extractCourseReserves(tgtCourseID string, docs []searchHit) []*courseSearchResponse {
	log.Printf("INFO: extract instructor course reserves for %s", tgtCourseID)
	out := make([]*courseSearchResponse, 0)
	for _, doc := range docs {
		for _, reserve := range doc.ReserveInfo {
			// format: courseID | courseName | instructor
			reserveInfo := strings.Split(reserve, "|")
			courseID := reserveInfo[0]
			courseName := reserveInfo[1]
			instructor := reserveInfo[2]

			if strings.Index(strings.ToLower(courseID), strings.ToLower(tgtCourseID)) != 0 {
				continue
			}

			log.Printf("INFO: process item %s reserve %s", doc.ID, reserve)
			item := reserveItem{ID: doc.ID, Title: doc.Title[0],
				Author:     strings.Join(doc.Author, "; "),
				CallNumber: strings.Join(doc.CallNumber, ", ")}

			// find existing course
			var tgtCourse *courseSearchResponse
			for _, csr := range out {
				if csr.CourseID == courseID {
					log.Printf("INFO: found existing record for course %s", courseID)
					tgtCourse = csr
					break
				}
			}
			if tgtCourse == nil {
				log.Printf("INFO: create new record for course %s", courseID)
				newCourse := courseSearchResponse{CourseID: courseID, CourseName: courseName}
				tgtCourse = &newCourse
				out = append(out, tgtCourse)
			}

			found := false
			for _, inst := range tgtCourse.Instructors {
				if inst.InstructorName == instructor {
					found = true
					if itemExists(inst.Items, item.ID) == false {
						log.Printf("INFO: append item to existing instructor...")
						inst.Items = append(inst.Items, item)
						break
					}
				}
			}

			if found == false {
				log.Printf("INFO: create new record for instructor %s", instructor)
				newInst := instructorItems{InstructorName: instructor}
				newInst.Items = append(newInst.Items, item)
				tgtCourse.Instructors = append(tgtCourse.Instructors, &newInst)
				log.Printf("INFO: new instructor: %v", newInst)
			}
		}
	}

	for _, csr := range out {
		sort.Slice(csr.Instructors, func(i, j int) bool {
			return csr.Instructors[i].InstructorName < csr.Instructors[j].InstructorName
		})
		for _, inst := range csr.Instructors {
			sort.Slice(inst.Items, func(i, j int) bool {
				return inst.Items[i].Title < inst.Items[j].Title
			})
		}
	}

	return out
}

func extractInstructorReserves(tgtInstructor string, docs []searchHit) []*instructorSearchResponse {
	log.Printf("INFO: extract course course reserves instructor %s", tgtInstructor)
	out := make([]*instructorSearchResponse, 0)
	for _, doc := range docs {
		for _, reserve := range doc.ReserveInfo {
			// format: courseID | courseName | instructor
			reserveInfo := strings.Split(reserve, "|")
			courseID := reserveInfo[0]
			courseName := reserveInfo[1]
			instructor := reserveInfo[2]
			if strings.Index(strings.ToLower(instructor), strings.ToLower(tgtInstructor)) != 0 {
				continue
			}

			log.Printf("INFO: process item %s reserve %s", doc.ID, reserve)
			item := reserveItem{ID: doc.ID, Title: doc.Title[0],
				Author:     strings.Join(doc.Author, "; "),
				CallNumber: strings.Join(doc.CallNumber, ", ")}

			// find existing instructor
			var tgtInstructor *instructorSearchResponse
			for _, isr := range out {
				if isr.InstructorName == instructor {
					tgtInstructor = isr
					break
				}
			}
			if tgtInstructor == nil {
				// log.Printf("INFO: create new record for instructor %s", instructor)
				newInstructor := instructorSearchResponse{InstructorName: instructor}
				tgtInstructor = &newInstructor
				out = append(out, tgtInstructor)
			}

			found := false
			for _, course := range tgtInstructor.Courses {
				if course.CourseID == courseID {
					found = true
					if itemExists(course.Items, item.ID) == false {
						// log.Printf("INFO: append item to existing course...")
						course.Items = append(course.Items, item)
						break
					}
				}
			}

			if found == false {
				// log.Printf("INFO: create new record for course %s", courseID)
				newCourse := courseItems{CourseID: courseID, CourseName: courseName}
				newCourse.Items = append(newCourse.Items, item)
				tgtInstructor.Courses = append(tgtInstructor.Courses, &newCourse)
			}
		}
	}

	for _, isr := range out {
		sort.Slice(isr.Courses, func(i, j int) bool {
			return isr.Courses[i].CourseID < isr.Courses[j].CourseID
		})
		for _, crs := range isr.Courses {
			sort.Slice(crs.Items, func(i, j int) bool {
				return crs.Items[i].Title < crs.Items[j].Title
			})
		}
	}

	return out
}

func itemExists(items []reserveItem, id string) bool {
	for _, i := range items {
		if i.ID == id {
			return true
		}
	}
	return false
}
