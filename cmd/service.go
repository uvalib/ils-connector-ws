package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
)

type sirsiSigniResponse struct {
	StaffKey     string `json:"staffKey"`
	SessionToken string `json:"sessionToken"`
}

type sirsiKey struct {
	Key string `json:"key"`
}

type sirsiDescription struct {
	Description string `json:"description"`
}

type sirsiMessageList struct {
	MessageList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messageList"`
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

	err := ctx.sirsiLogin()
	if err != nil {
		log.Printf("ERROR: %s", err.Error())
	}

	return &ctx, nil
}

func (svc *serviceContext) sirsiLogin() error {
	log.Printf("INFO: attempting sirsi login...")
	startTime := time.Now()
	svc.SirsiSession.SessionToken = ""
	svc.SirsiSession.StaffKey = ""
	url := fmt.Sprintf("%s/user/staff/login", svc.SirsiConfig.WebServicesURL)
	payloadOBJ := struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}{
		Login:    svc.SirsiConfig.User,
		Password: svc.SirsiConfig.Password,
	}
	payloadBytes, _ := json.Marshal(payloadOBJ)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(req, "STAFF", "")
	rawResp, rawErr := svc.HTTPClient.Do(req)
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
	// NO-OP
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
	c.JSON(http.StatusOK, vMap)
}

func (svc *serviceContext) healthCheck(c *gin.Context) {
	type hcResp struct {
		Healthy bool   `json:"healthy"`
		Message string `json:"message,omitempty"`
		Version int    `json:"version,omitempty"`
	}
	hcMap := make(map[string]hcResp)

	// sirsi healthcheck
	sirsiUnavailable := false
	sirsiSignedIn := true
	if svc.SirsiSession.SessionToken == "" || svc.SirsiSession.isExpired() {
		err := svc.sirsiLogin()
		if err != nil {
			log.Printf("ERROR: %s", err.Error())
			hcMap["sirsi"] = hcResp{Healthy: false, Message: err.Error()}
			sirsiSignedIn = false
			if strings.Contains(err.Error(), "503") {
				sirsiUnavailable = true
			}
		}
	}
	if sirsiSignedIn {
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

	// pda healthcheck
	pdaURL := fmt.Sprintf("%s/healthcheck", svc.PDAURL)
	_, pdaErr := svc.serviceGet(pdaURL, "")
	if pdaErr != nil {
		hcMap["pda"] = hcResp{Healthy: false, Message: pdaErr.string()}
	} else {
		hcMap["pda"] = hcResp{Healthy: true}
	}

	if sirsiUnavailable {
		c.JSON(http.StatusInternalServerError, hcMap)
	} else {
		c.JSON(http.StatusOK, hcMap)
	}
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
	rawResp, rawErr := svc.HTTPClient.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: %s processed in %d (ms)", url, elapsedMS)
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
	log.Printf("INFO: %s processed in %d (ms)", url, elapsedMS)
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
	log.Printf("INFO: %s processed in %d (ms)", url, elapsedMS)
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
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		status := resp.StatusCode
		errMsg := string(bodyBytes)
		return nil, &requestError{StatusCode: status, Message: errMsg}
	}

	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	return bodyBytes, nil
}
