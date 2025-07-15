package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/uvalib/virgo4-jwt/v4jwt"
	"gopkg.in/gomail.v2"
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
	Version            string
	SirsiConfig        sirsiConfig
	Solr               solrConfig
	SMTP               smtpConfig
	HSILLiadURL        string
	CourseReserveEmail string
	LawReserveEmail    string
	SirsiSession       sirsiSessionData
	Locations          locationContext
	Libraries          libraryContext
	Secrets            secretsConfig
	VirgoURL           string
	PDAURL             string
	UserInfoURL        string
	HTTPClient         *http.Client
	SlowHTTPClient     *http.Client
}

type requestError struct {
	StatusCode int
	Message    string
}

func (re *requestError) string() string {
	return fmt.Sprintf("%d: %s", re.StatusCode, re.Message)
}

type emailRequest struct {
	Subject string
	To      []string
	ReplyTo string
	CC      string
	From    string
	Body    string
}

func intializeService(version string, cfg *serviceConfig) (*serviceContext, error) {
	ctx := serviceContext{Version: version,
		SirsiConfig:        cfg.Sirsi,
		Solr:               cfg.Solr,
		SMTP:               cfg.SMTP,
		HSILLiadURL:        cfg.HSILLiadURL,
		CourseReserveEmail: cfg.CourseReserveEmail,
		LawReserveEmail:    cfg.LawReserveEmail,
		Secrets:            cfg.Secrets,
		PDAURL:             cfg.PDAURL,
		VirgoURL:           cfg.VirgoURL,
		UserInfoURL:        cfg.UserInfoURL,
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
	log.Printf("INFO: attempting sirsi login for %s", svc.SirsiConfig.User)
	svc.SirsiSession.SessionToken = ""
	svc.SirsiSession.StaffKey = ""
	payloadOBJ := sirsiStaffLoginReq{
		Login:    svc.SirsiConfig.User,
		Password: svc.SirsiConfig.Password,
	}

	resp, err := svc.sirsiPost(svc.HTTPClient, "/user/staff/login", payloadOBJ)
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

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/healthcheck", svc.UserInfoURL), nil)
	_, userErr := svc.sendRequest("user-ws", svc.HTTPClient, req)
	if userErr != nil {
		hcMap["userinfo"] = hcResp{Healthy: false, Message: userErr.string()}
	} else {
		hcMap["userinfo"] = hcResp{Healthy: true}
	}

	c.JSON(http.StatusOK, hcMap)
}

func (svc *serviceContext) sirsiGet(client *http.Client, uri string) ([]byte, *requestError) {
	url := fmt.Sprintf("%s%s", svc.SirsiConfig.WebServicesURL, uri)
	req, _ := http.NewRequest("GET", url, nil)
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	return svc.sendRequest("sirsi", client, req)
}

func (svc *serviceContext) sirsiDelete(client *http.Client, uri string) ([]byte, *requestError) {
	url := fmt.Sprintf("%s%s", svc.SirsiConfig.WebServicesURL, uri)
	req, _ := http.NewRequest("DELETE", url, nil)
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	return svc.sendRequest("sirsi", client, req)
}

func (svc *serviceContext) sirsiPost(client *http.Client, uri string, data interface{}) ([]byte, *requestError) {
	url := fmt.Sprintf("%s%s", svc.SirsiConfig.WebServicesURL, uri)
	b, _ := json.Marshal(data)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(b))
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	return svc.sendRequest("sirsi", client, req)
}

func (svc *serviceContext) setSirsiHeaders(req *http.Request, role string, authToken string) {
	req.Header.Set("x-sirs-clientID", svc.SirsiConfig.ClientID)
	req.Header.Set("x-sirs-locale", "en_US")
	req.Header.Set("SD-Originating-App-Id", "Virgo")
	if role != "" {
		req.Header.Set("SD-Preferred-Role", role)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if authToken != "" {
		req.Header.Set("x-sirs-sessionToken", authToken)
	}
	// jsonH, _ := json.Marshal(req.Header)
	// log.Printf("HEADERS: %s", jsonH)
}

func (svc *serviceContext) sendRequest(serviceName string, httpClient *http.Client, request *http.Request) ([]byte, *requestError) {
	log.Printf("INFO: %s %s request: %s", serviceName, request.Method, request.URL.String())
	startTime := time.Now()
	request.Header.Set("User-Agent", "Golang_ILS_Connector") // NOTE: required or sirsi responds with 403
	rawResp, rawErr := httpClient.Do(request)

	var reqErr *requestError
	var respBytes []byte

	if rawErr != nil {
		status := http.StatusBadRequest
		errMsg := rawErr.Error()
		if strings.Contains(rawErr.Error(), "Timeout") {
			status = http.StatusRequestTimeout
			errMsg = fmt.Sprintf("%s timed out", request.URL.String())
		} else if strings.Contains(rawErr.Error(), "connection refused") {
			status = http.StatusServiceUnavailable
			errMsg = fmt.Sprintf("%s refused connection", request.URL.String())
		}
		return nil, &requestError{StatusCode: status, Message: errMsg}
	}

	respBytes, _ = io.ReadAll(rawResp.Body)
	rawResp.Body.Close()
	if rawResp.StatusCode != http.StatusOK && rawResp.StatusCode != http.StatusCreated {
		status := rawResp.StatusCode
		errMsg := string(respBytes)
		respBytes = nil
		return nil, &requestError{StatusCode: status, Message: errMsg}
	}

	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	log.Printf("INFO: %s %s request processed in %d (ms)", serviceName, request.Method, elapsedMS)
	// log.Printf("INFO: %s %s response: %s", serviceName, request.Method, respBytes)

	return respBytes, reqErr
}

func (svc *serviceContext) handleSirsiErrorResponse(errResp *requestError) (*sirsiError, *requestError) {
	// check the error response for messageList data. If it is present, this is not considered a
	// system error and should be logged as an informative message. If it is not present, return an
	// errror so it can be logged as a system error with an ERROR tag
	if strings.Contains(errResp.Message, "messageList") {
		log.Printf("INFO: extract message list from failed sirsi response %s", errResp.string())
		var parsedErr sirsiError
		parseErr := json.Unmarshal([]byte(errResp.Message), &parsedErr)
		if parseErr != nil {
			newErr := requestError{StatusCode: http.StatusInternalServerError, Message: parseErr.Error()}
			return nil, &newErr
		}
		return &parsedErr, nil
	}
	return nil, errResp
}

func (svc *serviceContext) sendEmail(request *emailRequest) error {
	mail := gomail.NewMessage()
	mail.SetHeader("MIME-version", "1.0")
	mail.SetHeader("Content-Type", "text/plain; charset=\"UTF-8\"")
	mail.SetHeader("Subject", request.Subject)
	mail.SetHeader("To", request.To...)
	mail.SetHeader("From", request.From)
	if request.ReplyTo != "" {
		mail.SetHeader("Reply-To", request.ReplyTo)
	}
	if len(request.CC) > 0 {
		mail.SetHeader("Cc", request.CC)
	}
	mail.SetBody("text/plain", request.Body)

	if svc.SMTP.DevMode {
		log.Printf("Email is in dev mode. Logging message instead of sending")
		log.Printf("==================================================")
		mail.WriteTo(log.Writer())
		log.Printf("==================================================")
		return nil
	}

	log.Printf("Sending %s email to %s", request.Subject, strings.Join(request.To, ","))
	if svc.SMTP.Pass != "" {
		dialer := gomail.Dialer{Host: svc.SMTP.Host, Port: svc.SMTP.Port, Username: svc.SMTP.User, Password: svc.SMTP.Pass}
		dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		return dialer.DialAndSend(mail)
	}

	log.Printf("Sending email with no auth")
	dialer := gomail.Dialer{Host: svc.SMTP.Host, Port: svc.SMTP.Port}
	dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	return dialer.DialAndSend(mail)
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

func (svc *serviceContext) mintUserServiceJWT() string {
	expirationTime := time.Now().Add(5 * time.Minute)
	claims := jwt.StandardClaims{
		ExpiresAt: expirationTime.Unix(),
		Issuer:    "ilsconector",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedJWT, err := token.SignedString([]byte(svc.Secrets.UserJWTKey))
	if err != nil {
		log.Printf("ERROR: unable to mint one-time access token: %s", err.Error())
		return ""
	}
	return signedJWT
}

func cleanCatKey(catKey string) string {
	re := regexp.MustCompile("^u")
	return re.ReplaceAllString(catKey, "")
}

func loadDataFile(filename string) []string {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("ERROR: unable to load %s: %s", filename, err.Error())
		return make([]string, 0)
	}
	return strings.Split(string(bytes), "\n")
}

func listContains(list []string, tgt string) bool {
	found := false
	for _, val := range list {
		if val == tgt {
			found = true
			break
		}
	}
	return found
}
