package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/uvalib/virgo4-jwt/v4jwt"
)

type sirsiStaffLoginReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type sirsiSigniResponse struct {
	StaffKey          string `json:"staffKey"`
	PinCreateDate     string `json:"pinCreateDate"`
	PinExpirationDate any    `json:"pinExpirationDate"`
	Name              string `json:"name"`
	SessionToken      string `json:"sessionToken"`
	Message           any    `json:"message"`
}

type sirsiKey struct {
	Resource string `json:"resource"`
	Key      string `json:"key"`
}

type sirsiDescription struct {
	Description string `json:"description"`
}

type sirsiMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type sirsiMessageList struct {
	MessageList []sirsiMessage `json:"messageList"`
}

type sirsiError struct {
	MessageList    []sirsiMessage `json:"messageList"`
	DescribeURI    string         `json:"describeUri"`
	PromptRequired bool           `json:"promptRequired"`
	DataMap        struct {
		PromptType         string `json:"promptType"`
		RecommendedAction  string `json:"recommendedAction"`
		PromptRequiresData bool   `json:"promptRequiresData"`
		PromptDataType     string `json:"promptDataType"`
	} `json:"dataMap"`
}

type sirsiSessionData struct {
	StaffKey     string
	SessionToken string
	RefreshAt    time.Time
}

func (sess *sirsiSessionData) isExpired() bool {
	return time.Now().After(sess.RefreshAt)
}

type serviceContext struct {
	Version        string
	SirsiConfig    sirsiConfig
	SirsiSession   sirsiSessionData
	Locations      locationContext
	Libraries      libraryContext
	Secrets        secretsConfig
	VirgoURL       string
	PDAURL         string
	UserInfoURL    string
	HTTPClient     *http.Client
	SlowHTTPClient *http.Client
}

type requestError struct {
	StatusCode int
	Message    string
}

func (re *requestError) string() string {
	return fmt.Sprintf("%d: %s", re.StatusCode, re.Message)
}

func intializeService(version string, cfg *serviceConfig) (*serviceContext, error) {
	ctx := serviceContext{Version: version,
		SirsiConfig: cfg.Sirsi,
		Secrets:     cfg.Secrets,
		PDAURL:      cfg.PDAURL,
		VirgoURL:    cfg.VirgoURL,
		UserInfoURL: cfg.UserInfoURL,
	}

	log.Printf("INFO: create http client for external service calls")
	defaultTransport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 600 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
	}
	ctx.HTTPClient = &http.Client{
		Transport: defaultTransport,
		Timeout:   10 * time.Second,
	}
	ctx.SlowHTTPClient = &http.Client{
		Transport: defaultTransport,
		Timeout:   30 * time.Second,
	}

	return &ctx, nil
}

func (svc *serviceContext) terminateSession() {
	if svc.SirsiSession.SessionToken != "" && svc.SirsiSession.isExpired() == false {
		log.Printf("INFO: terminate active sirsi session")
		empty := struct{}{}
		_, err := svc.sirsiPost(svc.HTTPClient, "/user/staff/logout", empty)
		svc.SirsiSession.SessionToken = ""
		svc.SirsiSession.StaffKey = ""
		if err != nil {
			log.Printf("ERROR: unable to end session: %s", err.string())
		} else {
			log.Printf("INFO: sirsi session ended")
		}
	} else {
		log.Printf("INFO: no active sirsi session; ok to terminate")
	}
}

func (svc *serviceContext) sirsiLogin() error {
	log.Printf("INFO: attempting sirsi login...")
	startTime := time.Now()
	svc.SirsiSession.SessionToken = ""
	svc.SirsiSession.StaffKey = ""
	url := fmt.Sprintf("%s/user/staff/login", svc.SirsiConfig.WebServicesURL)
	payloadOBJ := sirsiStaffLoginReq{
		Login:    svc.SirsiConfig.User,
		Password: svc.SirsiConfig.Password,
	}
	cut := fmt.Sprintf("%s***", payloadOBJ.Password[0:4])
	log.Printf("INFO: login user %s, partial pass: %s", payloadOBJ.Login, cut)
	payloadBytes, _ := json.Marshal(payloadOBJ)
	req, reqErr := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if reqErr != nil {
		return fmt.Errorf("unable to create new post request: %s", reqErr.Error())
	}
	svc.setSirsiHeaders(req, "STAFF", "")
	log.Printf("INFO: login headers %+v", req.Header)
	rawResp, rawErr := svc.HTTPClient.Do(req)
	if rawErr != nil {
		log.Printf("ERROR: login failed; raw error: %s", rawErr.Error())
	}
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: login request processed; elapsed time: %d (ms)", elapsedMS)
	if err != nil {
		return fmt.Errorf("sirsi login failed with %d: %s", err.StatusCode, err.Message)
	}

	var respObj sirsiSigniResponse
	parseErr := json.Unmarshal(resp, &respObj)
	if parseErr != nil {
		return fmt.Errorf("unable to parse login response: %s", parseErr.Error())
	}

	svc.SirsiSession.SessionToken = respObj.SessionToken
	svc.SirsiSession.StaffKey = respObj.StaffKey
	svc.SirsiSession.RefreshAt = time.Now().Add(1 * time.Hour)
	log.Printf("INFO: sirsi login success; refresh at %s", svc.SirsiSession.RefreshAt.String())

	return nil
}

// ignoreFavicon is a dummy to handle browser favicon requests without warnings
func (svc *serviceContext) ignoreFavicon(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// GetVersion reports the version of the serivce
func (svc *serviceContext) getVersion(c *gin.Context) {
	build := "unknown"
	// cos our CWD is the bin directory
	files, _ := filepath.Glob("../buildtag.*")
	if len(files) == 1 {
		build = strings.Replace(files[0], "../buildtag.", "", 1)
	}

	vMap := make(map[string]string)
	vMap["version"] = svc.Version
	vMap["build"] = build
	log.Printf("INFO: version %+v", vMap)
	c.JSON(http.StatusOK, vMap)
}

func (svc *serviceContext) healthCheck(c *gin.Context) {
	type hcResp struct {
		Healthy bool   `json:"healthy"`
		Message string `json:"message,omitempty"`
		Version int    `json:"version,omitempty"`
	}
	hcMap := make(map[string]hcResp)

	if svc.SirsiSession.SessionToken != "" && svc.SirsiSession.isExpired() == false {
		url := fmt.Sprintf("/user/staff/key/%s", svc.SirsiSession.StaffKey)
		_, err := svc.sirsiGet(svc.HTTPClient, url)
		if err != nil {
			hcMap["sirsi"] = hcResp{Healthy: false, Message: err.string()}
		} else {
			hcMap["sirsi"] = hcResp{Healthy: true}
		}
	}

	// user service healthcheck
	userURL := fmt.Sprintf("%s/healthcheck", svc.UserInfoURL)
	_, userErr := svc.serviceGet(userURL, "")
	if userErr != nil {
		hcMap["userinfo"] = hcResp{Healthy: false, Message: userErr.string()}
	} else {
		hcMap["userinfo"] = hcResp{Healthy: true}
	}

	c.JSON(http.StatusOK, hcMap)
}

func (svc *serviceContext) serviceGet(url string, secret string) ([]byte, *requestError) {
	log.Printf("INFO: service get request: %s", url)
	startTime := time.Now()
	if secret != "" {
		jwt, err := mintBasicJWT(secret)
		if err != nil {
			log.Printf("ERROR: unable to mint temporary access jwt: %s", err.Error())
			return nil, &requestError{StatusCode: http.StatusInternalServerError, Message: err.Error()}
		}
		url += fmt.Sprintf("?auth=%s", jwt)
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Golang_ILS_Connector") // NOTE: required or sirsi responds with 403
	rawResp, rawErr := svc.HTTPClient.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: get response: %s", resp)
	log.Printf("INFO: request processed in %d (ms)", elapsedMS)
	return resp, err
}

func (svc *serviceContext) sirsiGet(client *http.Client, uri string) ([]byte, *requestError) {
	url := fmt.Sprintf("%s%s", svc.SirsiConfig.WebServicesURL, uri)
	log.Printf("INFO: sirsi get request: %s", url)
	startTime := time.Now()
	req, _ := http.NewRequest("GET", url, nil)
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	rawResp, rawErr := client.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: sirsi response: %s", resp)
	log.Printf("INFO: sirsi request processed in %d (ms)", elapsedMS)
	return resp, err
}

func (svc *serviceContext) sirsiDelete(client *http.Client, uri string) ([]byte, *requestError) {
	url := fmt.Sprintf("%s%s", svc.SirsiConfig.WebServicesURL, uri)
	log.Printf("INFO: sirsi delete request: %s", url)
	startTime := time.Now()
	req, _ := http.NewRequest("DELETE", url, nil)
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	rawResp, rawErr := client.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: sirsi response: %s", resp)
	log.Printf("INFO: sirsi request processed in %d (ms)", elapsedMS)
	return resp, err
}

func (svc *serviceContext) sirsiPost(client *http.Client, uri string, data interface{}) ([]byte, *requestError) {
	url := fmt.Sprintf("%s%s", svc.SirsiConfig.WebServicesURL, uri)
	log.Printf("INFO: sirsi post request: %s", url)
	startTime := time.Now()
	b, _ := json.Marshal(data)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(b))
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	rawResp, rawErr := client.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: sirsi response: %s", resp)
	log.Printf("INFO: sirsi request processed in %d (ms)", elapsedMS)
	return resp, err
}

func (svc *serviceContext) setSirsiHeaders(req *http.Request, role string, authToken string) {
	req.Header.Set("x-sirs-clientID", svc.SirsiConfig.ClientID)
	req.Header.Set("x-sirs-locale", "en_US")
	req.Header.Set("SD-Originating-App-Id", "Virgo")
	req.Header.Set("SD-Preferred-Role", role)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Golang_ILS_Connector") // NOTE: required or sirsi responds with 403
	if authToken != "" {
		req.Header.Set("x-sirs-sessionToken", authToken)
	}
}

func getVirgoClaims(c *gin.Context) (*v4jwt.V4Claims, error) {
	claims, exist := c.Get("claims")
	if exist == false {
		log.Printf("ERROR: no claims found for user requesting a hold")
		return nil, fmt.Errorf("request is not authorized")
	}
	v4Claims, ok := claims.(*v4jwt.V4Claims)
	if !ok {
		log.Printf("ERROR: invalid claims found for user requesting a hold")
		return nil, fmt.Errorf("request is not authorized")
	}
	return v4Claims, nil
}

func mintBasicJWT(secret string) (string, error) {
	expirationTime := time.Now().Add(5 * time.Minute)
	claims := jwt.StandardClaims{
		ExpiresAt: expirationTime.Unix(),
		Issuer:    "ilsconector",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func handleAPIResponse(tgtURL string, resp *http.Response, err error) ([]byte, *requestError) {
	if err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.Contains(err.Error(), "Timeout") {
			status = http.StatusRequestTimeout
			errMsg = fmt.Sprintf("%s timed out", tgtURL)
		} else if strings.Contains(err.Error(), "connection refused") {
			status = http.StatusServiceUnavailable
			errMsg = fmt.Sprintf("%s refused connection", tgtURL)
		}
		return nil, &requestError{StatusCode: status, Message: errMsg}
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		status := resp.StatusCode
		errMsg := string(bodyBytes)
		return nil, &requestError{StatusCode: status, Message: errMsg}
	}

	return bodyBytes, nil
}

func loadDataFile(filename string) []string {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("ERROR: unable to load %s: %s", filename, err.Error())
		return make([]string, 0)
	}
	return strings.Split(string(bytes), "\n")
}
