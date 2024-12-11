package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (svc *serviceContext) checkPassword(c *gin.Context) {
	var passReq struct {
		ComputeID string `json:"computeID"`
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
		log.Printf("ERROR: check pass for %s  failed: %s", passReq.ComputeID, sirsiErr.string())
		c.String(http.StatusUnauthorized, "invalid")
		return
	}
	c.String(http.StatusOK, "valid")
}

func (svc *serviceContext) changePassword(c *gin.Context) {
	var passReq struct {
		CurrPass  string `json:"currPassword"`
		NewPass   string `json:"newPassword"`
		ComputeID string `json:"computeID"`
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
		Password: passReq.CurrPass,
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
		NewPass:  passReq.NewPass,
		CurrPass: passReq.CurrPass,
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
