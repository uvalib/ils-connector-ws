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
)

type sirsiSessionData struct {
	StaffKey     string
	SessionToken string
	RefreshAt    time.Time
}

type serviceContext struct {
	Version      string
	SirsiConfig  sirsiConfig
	SirsiSession sirsiSessionData
	Secrets      secretsConfig
	HTTPClient   *http.Client
}

type requestError struct {
	StatusCode int
	Message    string
}

func intializeService(version string, cfg *serviceConfig) (*serviceContext, error) {
	ctx := serviceContext{Version: version,
		SirsiConfig: cfg.Sirsi,
		Secrets:     cfg.Secrets,
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

	err := ctx.sirsiLogin()
	if err != nil {
		log.Printf("ERROR: %s", err.Error())
	}

	return &ctx, nil
}

func (svc *serviceContext) sirsiLogin() error {
	log.Printf("INFO: attempting sirsi login...")
	startTime := time.Now()
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
	req.Header.Set("x-sirs-clientID", svc.SirsiConfig.ClientID)
	req.Header.Set("x-sirs-locale", "en_US")
	req.Header.Set("SD-Originating-App-Id", "Virgo")
	req.Header.Set("SD-Preferred-Role", "STAFF")
	req.Header.Set("SD-Working-LibraryID", svc.SirsiConfig.Library)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Golang_ILS_Connector") // NOTE: required or sirsi responds with 403
	rawResp, rawErr := svc.HTTPClient.Do(req)
	resp, err := handleAPIResponse(url, rawResp, rawErr)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: login request processed; elapsed time: %d (ms)", elapsedMS)
	if err != nil {
		return fmt.Errorf("sirsi login failed with %d: %s", err.StatusCode, err.Message)
	}

	type signinResp struct {
		StaffKey     string `json:"staffKey"`
		SessionToken string `json:"sessionToken"`
	}
	var respObj signinResp
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
	log.Printf("Got healthcheck request")
	type hcResp struct {
		Healthy bool   `json:"healthy"`
		Message string `json:"message,omitempty"`
		Version int    `json:"version,omitempty"`
	}
	hcMap := make(map[string]hcResp)

	c.JSON(http.StatusOK, hcMap)
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
