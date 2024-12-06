package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type userInfoRespData struct {
	Status  uint64 `json:"status"`
	Message string `json:"message"`
	User    struct {
		CID         string   `json:"cid"`
		Title       []string `json:"title"`
		Department  []string `json:"department"`
		Description []string
		Office      []string `json:"office"`
		Private     string   `json:"private"`
	} `json:"user"`
}

type sirsiUserSearchResp struct {
	TotalResults uint64          `json:"totalResults"`
	Result       []sirsiUserData `json:"result"`
}

type sirsiAddressData struct {
	Key    string `json:"key"`
	Fields struct {
		Code struct {
			Key string `json:"key"`
		} `json:"code"`
		Data string `json:"data"`
	}
}

type sirsiUserData struct {
	Key    string `json:"key"`
	Fields struct {
		DisplayName    string `json:"displayName"`
		Barcode        string `json:"barcode"`
		FirstName      string `json:"firstName"`
		LastName       string `json:"lastName"`
		MiddleName     string `json:"middleName"`
		PreferredName  string `json:"preferredName"`
		PrimaryAddress struct {
			Fields struct {
				EmailAddress string `json:"emailAddress"`
			} `json:"fields"`
		} `json:"primaryAddress"`
		Profile struct {
			Key string `json:"key"`
		} `json:"profile"`
		PatronStatusInfo struct {
			Key    string `json:"key"`
			Fields struct {
				Standing struct {
					Key string `json:"key"`
				} `json:"standing"`
				AmountOwed struct {
					Amount string `json:"amount"`
				} `json:"amountOwed"`
			} `json:"fields"`
		} `json:"patronStatusInfo"`
		Library struct {
			Key string `json:"key"`
		} `json:"library"`
		Address1 []sirsiAddressData `json:"address1"`
		Address2 []sirsiAddressData `json:"address2"`
		Address3 []sirsiAddressData `json:"address3"`
	} `json:"fields"`
}

type userAddress struct {
	Line1 string `json:"line1"`
	Line2 string `json:"line2"`
	Line3 string `json:"line3"`
	Zip   string `json:"zip"`
	Phone string `json:"phone"`
}

type userDetails struct {
	ID            string `json:"id"`
	CommunityUser bool   `json:"communityUser"`
	Title         string `json:"title"`
	Department    string `json:"department"`
	Address       string `json:"address"`
	Private       string `json:"private"`
	Description   string `json:"description"`
	Barcode       string `json:"barcode"`
	Key           string `json:"key"`
	DisplayName   string `json:"displayName"`
	Email         string `json:"email"`
	NoAccount     bool   `json:"noAccount,omitempty"`
	SirsiProfile  struct {
		PreferredName string      `json:"preferredName"`
		FirstName     string      `json:"firstName"`
		MiddleName    string      `json:"middleName"`
		LastName      string      `json:"lastName"`
		Address1      userAddress `json:"address1"`
		Address2      userAddress `json:"address2"`
		Address3Email string      `json:"address3Email"`
	} `json:"sirsiProfile"`
	Profile     string `json:"profile"`
	Standing    string `json:"standing"`
	HomeLibrary string `json:"homeLibrary"`
	AmountOwed  string `json:"amountOwed"`
}

func (svc *serviceContext) getUserInfo(c *gin.Context) {
	computeID := c.Param("compute_id")
	if computeID == "" {
		c.String(http.StatusBadRequest, "compute_id is required")
		return
	}

	log.Printf("INFO: lookup user %s in user-ws", computeID)
	var user userDetails
	url := fmt.Sprintf("%s/user/%s", svc.UserInfoURL, computeID)
	raw, err := svc.serviceGet(url, svc.Secrets.AuthSharedSecret)
	if err != nil {
		log.Printf("ERROR: user request failed: %s", err.string())
		c.String(err.StatusCode, err.Message)
		return
	}

	if string(raw) != "" {
		log.Printf("INFO: parse user-ews response [%s]", raw)
		var userResp userInfoRespData
		err := json.Unmarshal(raw, &userResp)
		if err != nil {
			log.Printf("ERROR: unable to parse user-ws response for %s: %s", computeID, err.Error())
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		user.ID = userResp.User.CID
		user.CommunityUser = false
		if len(userResp.User.Title) > 0 {
			user.Title = userResp.User.Title[0]
		}
		if len(userResp.User.Department) > 0 {
			user.Department = strings.Join(userResp.User.Department, ", ")
		}
		if len(userResp.User.Office) > 0 {
			user.Address = userResp.User.Office[0]
		}
		if len(userResp.User.Description) > 0 {
			user.Description = strings.Join(userResp.User.Description, ", ")
		}
		user.Private = userResp.User.Private
	} else {
		log.Printf("INFO: user %s not in user-ws; flagging as community user", computeID)
		user.CommunityUser = true
	}

	log.Printf("INFO: lookup user %s in sirsi", computeID)
	fields := "barcode,primaryAddress{*},address1,address2,address3,displayName,preferredName,firstName,middleName,lastName,"
	fields += "profile,patronStatusInfo{standing,amountOwed},library"
	sirsiURL := fmt.Sprintf("/user/patron/search?q=ALT_ID:%s&includeFields=%s", computeID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(sirsiURL)
	if sirsiErr != nil {
		log.Printf("ERROR: get sirsi user %s failed: %s", computeID, sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}

	var sirsiResp sirsiUserSearchResp
	parseErr := json.Unmarshal(sirsiRaw, &sirsiResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse sirsi user %s response: %s", computeID, parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	if sirsiResp.TotalResults == 0 {
		log.Printf("INFO: user %s not found in sirsi", computeID)
		user.NoAccount = true
		c.JSON(http.StatusOK, user)
		return
	}
	if sirsiResp.TotalResults > 1 {
		log.Printf("INFO: %d sirsi users found for %s", sirsiResp.TotalResults, computeID)
		c.String(http.StatusBadRequest, fmt.Sprintf("multiple users match %s", computeID))
		return
	}

	userFields := sirsiResp.Result[0].Fields
	primaryAddrFields := userFields.PrimaryAddress.Fields
	statusFields := userFields.PatronStatusInfo.Fields

	user.Barcode = userFields.Barcode
	user.Key = userFields.PatronStatusInfo.Key
	user.DisplayName = userFields.DisplayName
	user.SirsiProfile.FirstName = userFields.FirstName
	user.SirsiProfile.MiddleName = userFields.MiddleName
	user.SirsiProfile.LastName = userFields.LastName
	user.Profile = userFields.Profile.Key
	user.HomeLibrary = userFields.Library.Key

	extractAddress(&user.SirsiProfile.Address1, userFields.Address1)
	extractAddress(&user.SirsiProfile.Address2, userFields.Address2)

	// addr3 is email only
	if len(userFields.Address3) > 1 {
		log.Printf("WARNING: sirsi address3 field does not follow convention: %+v", userFields.Address3)
	} else {
		for _, a3 := range userFields.Address3 {
			if a3.Fields.Code.Key == "EMAIL" {
				user.SirsiProfile.Address3Email = a3.Fields.Data
			}
		}
	}

	// Per Stephanie Hunter, DELINQUENT not a vailid state. Workflows run
	// every night to wipe it out. If one gets missed, change it to OK here.
	user.Standing = statusFields.Standing.Key
	if user.Standing == "DELINQUENT" {
		user.Standing = "OK"
	}
	user.AmountOwed = statusFields.AmountOwed.Amount

	user.Email = primaryAddrFields.EmailAddress
	if user.Email == "" {
		log.Printf("WARNING: %s does not have a sirsi email", computeID)
	}

	c.JSON(http.StatusOK, user)
}

func extractAddress(destAddr *userAddress, srcAddressData []sirsiAddressData) {
	for _, a1 := range srcAddressData {
		if a1.Fields.Code.Key == "LINE1" {
			destAddr.Line1 = a1.Fields.Data
		}
		if a1.Fields.Code.Key == "LINE2" {
			destAddr.Line2 = a1.Fields.Data
		}
		if a1.Fields.Code.Key == "LINE3" {
			destAddr.Line3 = a1.Fields.Data
		}
		if a1.Fields.Code.Key == "ZIP" {
			destAddr.Zip = a1.Fields.Data
		}
		if a1.Fields.Code.Key == "PHONE" {
			destAddr.Phone = a1.Fields.Data
		}
	}
}
