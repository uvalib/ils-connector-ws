package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type tmpAccount struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Password  string `json:"password"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Zip       string `json:"zip"`
}

type sirsiRegistration struct {
	FirstName        string `json:"patron-firstName"`
	LastName         string `json:"patron-lastName"`
	Password         string `json:"patron-pin"`
	Email            string `json:"patronAddress3-EMAIL"`
	Phone            string `json:"patronAddress1-PHONE"`
	AddressLine1     string `json:"patronAddress1-LINE1"`
	AddressLine2     string `json:"patronAddress1-LINE2"`
	AddressLine3     string `json:"patronAddress1-LINE3"` // city, state
	Zip              string `json:"patronAddress1-ZIP"`
	PreferredAddress string `json:"patron-preferredAddress"` // harcoded 3
	ActivationURL    string `json:"activationUrl"`
}

type sirsiRegistrationResponse struct {
	Patron       sirsiKey `json:"patron"`
	SessionToken string   `json:"sessionToken"`
	Barcode      string   `json:"barcode"`
}

type sirsiActivateResponse struct {
	Success bool `json:"success"`
}

func (r *sirsiRegistration) validate() error {
	var errors []string
	if r.FirstName == "" {
		errors = append(errors, "first name is reqired")
	}
	if r.LastName == "" {
		errors = append(errors, "last name is reqired")
	}
	if r.Password == "" {
		errors = append(errors, "password is reqired")
	}
	if r.Email == "" {
		errors = append(errors, "email is reqired")
	}
	if r.Phone == "" {
		errors = append(errors, "phone is reqired")
	}
	if r.AddressLine1 == "" {
		errors = append(errors, "address1 is reqired")
	}
	if r.AddressLine3 == "" {
		errors = append(errors, "city/state is reqired")
	}
	if r.Zip == "" {
		errors = append(errors, "zip is reqired")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, ","))
	}
	return nil
}

func (a tmpAccount) MarshalJSON() ([]byte, error) {
	m := map[string]string{
		"firstName": fmt.Sprintf("%s", a.FirstName),
		"lastName":  fmt.Sprintf("%s", a.LastName),
		"email":     fmt.Sprintf("%s", a.Email),
		"phone":     fmt.Sprintf("%s", a.Phone),
		"address1":  fmt.Sprintf("%s", a.Address1),
		"address2":  fmt.Sprintf("%s", a.Address2),
		"city":      fmt.Sprintf("%s", a.City),
		"state":     fmt.Sprintf("%s", a.State),
		"zip":       fmt.Sprintf("%s", a.Zip),
	}
	return json.Marshal(m)
}

//	curl --request POST  \
//	  --url http://localhost:8185/users/check_password \
//	  --header 'Content-Type: application/json' \
//	  --data '{"barcode": "C000011111", "password": "PASS"}'
func (svc *serviceContext) checkPassword(c *gin.Context) {
	var passReq struct {
		ComputeID string `json:"barcode"`
		Password  string `json:"password"`
	}
	err := c.ShouldBindJSON(&passReq)
	if err != nil {
		log.Printf("ERROR: Unable to parse check password request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("INFO: check password for %s", passReq.ComputeID)
	data := struct {
		AlternateID string `json:"alternateID"`
		Password    string `json:"password"`
	}{
		AlternateID: passReq.ComputeID,
		Password:    passReq.Password,
	}
	_, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/patron/authenticate", data)
	if sirsiErr != nil {
		if sirsiErr.StatusCode == 401 {
			// some accounts have the computeID in the barcode field.. try that
			log.Printf("INFO: alt id password check failed; try barcode")
			bcErr := svc.checkBarcodePassword(passReq.ComputeID, passReq.Password)
			if bcErr != nil {
				if bcErr.StatusCode == 401 {
					log.Printf("INFO: check pass for barcode %s failed: %s", passReq.ComputeID, sirsiErr.string())
					c.String(http.StatusUnauthorized, "invalid")
				} else {
					log.Printf("ERROR: check pass for barcode %s  failed: %s", passReq.ComputeID, sirsiErr.string())
					c.String(http.StatusInternalServerError, "invalid")
				}
			} else {
				c.String(http.StatusOK, "valid")
			}
		} else {
			log.Printf("ERROR: check pass for %s  failed: %s", passReq.ComputeID, sirsiErr.string())
			c.String(http.StatusInternalServerError, "invalid")
		}
		return
	}
	c.String(http.StatusOK, "valid")
}

func (svc *serviceContext) checkBarcodePassword(computeID, pass string) *requestError {
	data := struct {
		Barcode  string `json:"barcode"`
		Password string `json:"password"`
	}{
		Barcode:  computeID,
		Password: pass,
	}
	_, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/patron/authenticate", data)
	if sirsiErr != nil {
		return sirsiErr
	}
	return nil
}

//	curl --request POST  \
//	  --url http://localhost:8185/users/change_password \
//	  --header 'Content-Type: application/json' \
//	  --data '{"computeID": "C000011111", "currPassword": "PASS!", "newPassword": "NEW"}'
func (svc *serviceContext) changePassword(c *gin.Context) {
	var passReq struct {
		CurrPin   string `json:"current_pin"`
		NewPin    string `json:"new_pin"`
		ComputeID string `json:"barcode"`
	}
	err := c.ShouldBindJSON(&passReq)
	if err != nil {
		log.Printf("ERROR: Unable to parse change password request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("INFO: change password for %s; first sign in...", passReq.ComputeID)
	loginReq := struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}{
		Login:    passReq.ComputeID,
		Password: passReq.CurrPin,
	}

	loginResp, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/patron/login", loginReq)
	if sirsiErr != nil {
		log.Printf("ERROR: change pass for %s failed: %s", passReq.ComputeID, sirsiErr.string())
		if sirsiErr.StatusCode == 401 {
			c.String(http.StatusUnauthorized, "incorrect password")
		} else {
			c.String(sirsiErr.StatusCode, sirsiErr.Message)
		}
		return
	}

	var respObj sirsiSigniResponse
	parseErr := json.Unmarshal(loginResp, &respObj)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse %s login response: %s", passReq.ComputeID, parseErr)
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	log.Printf("INFO: %s signed in; change password...", passReq.ComputeID)
	changeReq := struct {
		NewPass  string `json:"newPin"`
		CurrPass string `json:"currentPin"`
	}{
		NewPass:  passReq.NewPin,
		CurrPass: passReq.CurrPin,
	}
	payloadBytes, _ := json.Marshal(changeReq)
	url := fmt.Sprintf("%s/user/patron/changeMyPin", svc.SirsiConfig.WebServicesURL)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(req, "PATRON", respObj.SessionToken)
	rawResp, rawErr := svc.HTTPClient.Do(req)
	_, changeErr := handleAPIResponse(url, rawResp, rawErr)
	if changeErr != nil {
		log.Printf("WARNING: %s password change failed: %s", passReq.ComputeID, changeErr.string())
		var msg sirsiMessageList
		err := json.Unmarshal([]byte(changeErr.Message), &msg)
		if err != nil {
			c.String(http.StatusInternalServerError, changeErr.string())
		} else {
			c.String(http.StatusUnauthorized, msg.MessageList[0].Message)
		}
		return
	}
	c.String(http.StatusOK, "password changed")
}

// curl -X POST http://localhost:8185/users/C000011111/forgot_password
func (svc *serviceContext) forgotPassword(c *gin.Context) {
	var req struct {
		UserBarcode string `json:"userBarcode"`
	}
	qpErr := c.ShouldBindJSON(&req)
	if qpErr != nil {
		log.Printf("ERROR: invalid forgot password payload: %v", qpErr)
		c.String(http.StatusBadRequest, "Invalid request")
		return
	}
	log.Printf("INFO: user %s forgot password", req.UserBarcode)
	data := struct {
		Login    string `json:"login"`
		ResetURL string `json:"resetPinUrl"`
	}{
		Login:    req.UserBarcode,
		ResetURL: fmt.Sprintf("%s/signin?token=<RESET_PIN_TOKEN>", svc.VirgoURL),
	}
	_, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/patron/resetMyPin", data)
	if sirsiErr != nil {
		log.Printf("ERROR: %s forgot password failed: %s", req.UserBarcode, sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}
	c.String(http.StatusOK, "ok")
}

//	curl --request POST  \
//		--url http://localhost:8185/users/change_password_with_token \
//		--header 'Content-Type: application/json' \
//		--data '{"reset_password_token": "7bbaN2fr1WDWueRLpq8bB8npsow4mJ8iK7ilXlAP64zq6g1jvZ", "new_password": "PASS"}'
func (svc *serviceContext) changePasswordWithToken(c *gin.Context) {
	var qp struct {
		Token   string `json:"reset_password_token"`
		NewPass string `json:"new_password"`
	}
	qpErr := c.ShouldBindJSON(&qp)
	if qpErr != nil {
		log.Printf("ERROR: invalid change password payload: %v", qpErr)
		c.String(http.StatusBadRequest, "Invalid request")
		return
	}

	data := struct {
		Token    string `json:"resetPinToken"`
		Password string `json:"newPin"`
	}{
		Token:    qp.Token,
		Password: qp.NewPass,
	}
	payloadBytes, _ := json.Marshal(data)
	url := fmt.Sprintf("%s/user/patron/changeMyPin", svc.SirsiConfig.WebServicesURL)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(req, "PATRON", "")
	rawResp, rawErr := svc.HTTPClient.Do(req)
	_, changeErr := handleAPIResponse(url, rawResp, rawErr)
	if changeErr != nil {
		log.Printf("WARNING: token password change failed: %s", changeErr.string())
		var msg sirsiMessageList
		err := json.Unmarshal([]byte(changeErr.Message), &msg)
		if err != nil {
			c.String(http.StatusInternalServerError, changeErr.string())
		} else {
			c.String(http.StatusUnauthorized, msg.MessageList[0].Message)
		}
		return
	}
	c.String(http.StatusOK, "token password changed")
}

//	curl --request POST  \
//		--url http://localhost:8185/users/register \
//		--header 'Content-Type: application/json' \
//		--data '{"firstName": "first1", "lastName": "last1", "password": "PASS",
//					"email": "louffoster@gmail.com", "phone": "N/A", "address1": "123 fake", "address2": "",
//					"city": "Charlottesville", "state": "VA", "zip":"220902"}'
func (svc *serviceContext) registerNewUser(c *gin.Context) {
	var tmpAcct tmpAccount
	qpErr := c.ShouldBindJSON(&tmpAcct)
	if qpErr != nil {
		log.Printf("ERROR: invalid change register user payload: %v", qpErr)
		c.String(http.StatusBadRequest, "Invalid request")
		return
	}
	newUserBytes, _ := json.Marshal(tmpAcct)
	log.Printf("INFO: register new user [%s]", newUserBytes)

	log.Printf("INFO: create sirsi register payload")
	payload := sirsiRegistration{
		FirstName:        tmpAcct.FirstName,
		LastName:         tmpAcct.LastName,
		Password:         tmpAcct.Password,
		Email:            tmpAcct.Email,
		Phone:            tmpAcct.Phone,
		AddressLine1:     tmpAcct.Address1,
		AddressLine2:     tmpAcct.Address2,
		AddressLine3:     fmt.Sprintf("%s, %s", tmpAcct.City, tmpAcct.State),
		Zip:              tmpAcct.Zip,
		PreferredAddress: "3",
		ActivationURL:    fmt.Sprintf("%s/api/activateTempAccount/", svc.VirgoURL),
	}
	err := payload.validate()
	if err != nil {
		log.Printf("INFO: bad register request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("INFO: post user registration")
	resp, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/patron/register", payload)
	if sirsiErr != nil {
		log.Printf("WARNING: token password change failed: %s", sirsiErr.string())
		var msg sirsiMessageList
		err := json.Unmarshal([]byte(sirsiErr.Message), &msg)
		if err != nil {
			c.String(http.StatusInternalServerError, sirsiErr.string())
		} else {
			c.String(http.StatusBadRequest, msg.MessageList[0].Message)
		}
		return
	}

	var regResp sirsiRegistrationResponse
	parsErr := json.Unmarshal(resp, &regResp)
	if parsErr != nil {
		log.Printf("ERROR: unable to parse registration response: %s", parsErr.Error())
		c.String(http.StatusInternalServerError, parsErr.Error())
		return
	}

	// registration was successful, now update the altID with TEMP barcode
	log.Printf("INFO: update temp user %s (%s) registration with temp barcode and circhistory",
		regResp.Patron.Key, regResp.Barcode)
	idPayload := struct {
		Resource         string `json:"@resource"`
		Key              string `json:"@key"`
		AltID            string `json:"alternateID"`
		CircHistory      string `json:"keepCircHistory"`
		PreferredAddress string `json:"preferredAddress"`
	}{
		Resource:         "/user/patron",
		Key:              regResp.Patron.Key,
		AltID:            regResp.Barcode,
		CircHistory:      "CIRCRULE",
		PreferredAddress: "3",
	}

	payloadBytes, _ := json.Marshal(idPayload)
	url := fmt.Sprintf("%s/user/patron/key/%s", svc.SirsiConfig.WebServicesURL, idPayload.Key)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(payloadBytes))
	svc.setSirsiHeaders(req, "STAFF", svc.SirsiSession.SessionToken)
	req.Header.Set("Accept", "application/vnd.sirsidynix.roa.resource.v2+json")
	req.Header.Set("Content-Type", "application/vnd.sirsidynix.roa.resource.v2+json")
	req.Header.Set("SD-Working-LibraryID", svc.SirsiConfig.Library)
	rawResp, rawErr := svc.HTTPClient.Do(req)
	_, changeErr := handleAPIResponse(url, rawResp, rawErr)
	if changeErr != nil {
		log.Printf("WARNING: unable to update temp user %s: %s", regResp.Patron.Key, changeErr.string())
	}

	c.String(http.StatusOK, "registeration success")
}

func (svc *serviceContext) activateUser(c *gin.Context) {
	token := c.Param("token")
	log.Printf("INFO: activate new account with %s", token)
	req := struct {
		Token string `json:"activationToken"`
	}{
		Token: token,
	}
	resp, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/patron/activate", req)
	if sirsiErr != nil {
		log.Printf("ERROR: activate failed: %s", sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}

	var actResp sirsiActivateResponse
	parsErr := json.Unmarshal(resp, &actResp)
	if parsErr != nil {
		log.Printf("ERROR: unable to parse activate response: %s", parsErr)
		c.String(http.StatusInternalServerError, parsErr.Error())
		return
	}
	if actResp.Success == false {
		log.Printf("INFO: activate %s returned success=false", token)
		c.String(http.StatusUnprocessableEntity, "failed")
		return
	}
	c.String(http.StatusOK, "activated")
}

//	curl --request POST \
//	  --url http://localhost:8185/users/sirsi_staff_login \
//	  --header 'Accept: application/json' \
//	  --header 'Content-Type: application/json' \
//	  --data '{"username":"USER", "password": "PASS"}'
func (svc *serviceContext) staffLogin(c *gin.Context) {
	var loginReq struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	err := c.ShouldBindJSON(&loginReq)
	if err != nil {
		log.Printf("ERROR: Unable to parse staff login request: %s", err.Error())
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("INFO: staff %s login request", loginReq.Username)

	req := sirsiStaffLoginReq{
		Login:    loginReq.Username,
		Password: loginReq.Password,
	}
	resp, sirsiErr := svc.sirsiPost(svc.HTTPClient, "/user/staff/login", req)
	if sirsiErr != nil {
		log.Printf("ERROR: staff login failed: %s", sirsiErr.string())
		if sirsiErr.StatusCode == http.StatusUnauthorized {
			c.String(http.StatusUnauthorized, "invalid username or password")
		} else {
			c.String(sirsiErr.StatusCode, sirsiErr.Message)
		}
		return
	}

	var respObj sirsiSigniResponse
	parseErr := json.Unmarshal(resp, &respObj)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse loging reqponse for %s: %s", loginReq.Username, parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	log.Printf("INFO: %s logged in successfully", loginReq.Username)
	c.JSON(http.StatusOK, respObj)
}
