package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Raw user-ws response structures  =============================================

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

// Raw sirsi response structures  =============================================

type sirsiAddressData struct {
	Key    string `json:"key"`
	Fields struct {
		Code sirsiKey `json:"code"`
		Data string   `json:"data"`
	}
}

type sirsiBillItem struct {
	Fields struct {
		BlockList []struct {
			Fields struct {
				CreateDate string `json:"createDate"`
				Amount     struct {
					Amount string `json:"amount"`
				} `json:"amount"`
				Block struct {
					Fields sirsiDescription `json:"fields"`
				} `json:"block"`
				Item struct {
					Fields struct {
						Bib struct {
							Key    string `json:"key"`
							Fields struct {
								Author string `json:"author"`
							} `json:"fields"`
						} `json:"bib"`
						Barcode  string `json:"barcode"`
						ItemType struct {
							Fields sirsiDescription `json:"fields"`
						} `json:"itemType"`
					} `json:"fields"`
				} `json:"item"`
				Library struct {
					Fields sirsiDescription `json:"fields"`
				} `json:"library"`
				CallNumber string `json:"callNumber"`
				Title      string `json:"title"`
			} `json:"fields"`
		} `json:"blockList"`
	} `json:"fields"`
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
		Profile          sirsiKey `json:"profile"`
		PatronStatusInfo struct {
			Key    string `json:"key"`
			Fields struct {
				Standing   sirsiKey `json:"standing"`
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

type sirsiCheckoutBlocRec struct {
	Amount struct {
		Amount string `json:"amount"`
	} `json:"amount"`
	Block struct {
		Fields sirsiDescription `json:"fields"`
	} `json:"block"`
	Item sirsiKey `json:"item"`
}

type sirsiCheckout struct {
	Fields struct {
		CircRecordList []struct {
			Fields struct {
				Item struct {
					Key    string `json:"key"`
					Fields struct {
						Call struct {
							Fields struct {
								Bib struct {
									Key    string `json:"key"`
									Fields struct {
										Author string `json:"author"`
										Title  string `json:"title"`
									} `json:"fields"`
								} `json:"bib"`
								DispCallNumber string `json:"dispCallNumber"`
							} `json:"fields"`
						} `json:"call"`
						Barcode         string   `json:"barcode"`
						CurrentLocation sirsiKey `json:"currentLocation"`
					} `json:"fields"`
				} `json:"item"`
				DueDate string `json:"dueDate"`
				Library struct {
					Fields sirsiDescription `json:"fields"`
				} `json:"library"`
				Overdue                bool `json:"overdue"`
				EstimatedOverdueAmount struct {
					Amount string `json:"amount"`
				} `json:"estimatedOverdueAmount"`
				RecallDueDate string `json:"recallDueDate"`
				RenewalDate   string `json:"renewalDate"`
			} `json:"fields"`
		} `json:"circRecordList"`
		BlockList []struct {
			Fields sirsiCheckoutBlocRec `json:"fields"`
		} `json:"blockList"`
	} `json:"fields"`
}

type sirsiHolds struct {
	Key    string `json:"key"`
	Fields struct {
		HoldRecordList []struct {
			Key    string `json:"key"`
			Fields struct {
				Bib struct {
					Key    string `json:"key"`
					Fields struct {
						Author string `json:"author"`
						Title  string `json:"title"`
					} `json:"fields"`
				} `json:"bib"`
				Item struct {
					Key    string `json:"key"`
					Fields struct {
						Call struct {
							Key    string `json:"key"`
							Fields struct {
								DispCallNumber string `json:"dispCallNumber"`
							} `json:"fields"`
						} `json:"call"`
						Barcode         string   `json:"barcode"`
						CurrentLocation sirsiKey `json:"currentLocation"`
						Library         sirsiKey `json:"library"`
						Transit         struct {
							Fields struct {
								TransitReason string `json:"transitReason"`
							} `json:"fields"`
						} `json:"transit"`
					} `json:"fields"`
				} `json:"item"`
				BeingHeldDate string   `json:"beingHeldDate"`
				PickupLibrary sirsiKey `json:"pickupLibrary"`
				PlacedDate    string   `json:"placedDate"`
				QueueLength   uint64   `json:"queueLength"`
				QueuePosition uint64   `json:"queuePosition"`
				RecallStatus  string   `json:"recallStatus"`
				Status        string   `json:"status"`
			} `json:"fields"`
		} `json:"holdRecordList"`
	} `json:"fields"`
}

// ILSConnector response structures ===========================================

type userAddress struct {
	Line1 string `json:"line1"`
	Line2 string `json:"line2"`
	Line3 string `json:"line3"`
	Zip   string `json:"zip"`
	Phone string `json:"phone"`
}

type userDetails struct {
	ID            string `json:"id,omitempty"`
	CommunityUser bool   `json:"communityUser"`
	Title         string `json:"title,omitempty"`
	Department    string `json:"department,omitempty"`
	Address       string `json:"address,omitempty"`
	Private       string `json:"private,omitempty"`
	Description   string `json:"description,omitempty"`
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

type holdResponse struct {
	Holds []holdDetails `json:"holds"`
}

type holdDetails struct {
	ID             string `json:"id"`
	UserID         string `json:"userID"`
	PickupLocation string `json:"pickupLocation"`
	Status         string `json:"status"`
	PlacedDate     string `json:"placedDate"`
	QueueLength    uint64 `json:"queueLength"`
	QuePosition    uint64 `json:"queuePosition"`
	TitleKey       string `json:"titleKey"`
	Title          string `json:"title"`
	Author         string `json:"author"`
	CallNumber     string `json:"callNumber"`
	Barcode        string `json:"barcode"`
	ItemStatus     string `json:"itemStatus"`
	Cancellable    bool   `json:"cancellable"`
}

type checkoutBill struct {
	Amount string `json:"amount"`
	Label  string `json:"label"`
}

type checkoutDetails struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	Author          string         `json:"author"`
	Barcode         string         `json:"barcode"`
	CallNumber      string         `json:"callNumber"`
	Library         string         `json:"library"`
	CurrentLocation string         `json:"currentLocation"`
	Due             string         `json:"due"`
	OverDue         bool           `json:"overDue"`
	OverdueFee      string         `json:"overdueFee"`
	Bills           []checkoutBill `json:"bills"`
	RecallDueDate   string         `json:"recallDueDate"`
	RenewDate       string         `json:"renewDate"`
}

type billItem struct {
	Reason  string `json:"reason"`
	Amount  uint64 `json:"amount"`
	Library string `json:"library"`
	Date    string `json:"date"`
	Item    struct {
		ID         uint64 `json:"id"`
		Barcode    string `json:"barcode"`
		CallNumber string `json:"callNumber"`
		Type       string `json:"type"`
		Title      string `json:"title"`
		Author     string `json:"author"`
	} `json:"item"`
}

func (svc *serviceContext) getUserInfo(c *gin.Context) {
	computeID := c.Param("compute_id")
	log.Printf("INFO: lookup user %s in user-ws", computeID)
	var user userDetails
	url := fmt.Sprintf("%s/user/%s", svc.UserInfoURL, computeID)
	raw, err := svc.serviceGet(url, svc.Secrets.UserJWTKey)
	if err != nil {
		log.Printf("INFO: user %s not found in user-ws; flagging as community user", computeID)
		user.CommunityUser = true
	} else {
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
	}

	log.Printf("INFO: lookup user %s in sirsi", computeID)
	fields := "barcode,primaryAddress{*},address1,address2,address3,displayName,preferredName,firstName,middleName,lastName,"
	fields += "profile,patronStatusInfo{standing,amountOwed},library"
	sirsiURL := fmt.Sprintf("/user/patron/alternateID/%s?includeFields=%s", computeID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, sirsiURL)
	if sirsiErr != nil {
		log.Printf("ERROR: get sirsi user %s failed: %s", computeID, sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}

	var sirsiResp sirsiUserData
	parseErr := json.Unmarshal(sirsiRaw, &sirsiResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse sirsi user %s response: %s", computeID, parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	userFields := sirsiResp.Fields
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

func (svc *serviceContext) getUserBills(c *gin.Context) {
	computeID := c.Param("compute_id")
	log.Printf("INFO: get bills for %s", computeID)
	fields := "blockList{title,callNumber,amount,createDate,library{description},block{description},item{itemType{description},barcode,bib{author}}}"
	url := fmt.Sprintf("/user/patron/alternateID/%s?includeFields=%s", computeID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.HTTPClient, url)
	if sirsiErr != nil {
		log.Printf("ERROR: get sirsi user %s bills failed: %s", computeID, sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}

	var billResp sirsiBillItem
	parseErr := json.Unmarshal(sirsiRaw, &billResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse billd response: %s", parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	bills := make([]billItem, 0)
	for _, bl := range billResp.Fields.BlockList {
		bill := billItem{}
		bill.Date = bl.Fields.CreateDate
		amtF, _ := strconv.ParseFloat(bl.Fields.Amount.Amount, 64)
		bill.Amount = uint64(amtF)
		bill.Library = bl.Fields.Library.Fields.Description
		bill.Reason = bl.Fields.Block.Fields.Description
		bill.Item.ID, _ = strconv.ParseUint(bl.Fields.Item.Fields.Bib.Key, 10, 64)
		bill.Item.Type = bl.Fields.Item.Fields.ItemType.Fields.Description
		bill.Item.Barcode = bl.Fields.Item.Fields.Barcode
		bill.Item.CallNumber = bl.Fields.CallNumber
		bill.Item.Title = bl.Fields.Title
		bill.Item.Author = bl.Fields.Item.Fields.Bib.Fields.Author

		bills = append(bills, bill)
	}

	c.JSON(http.StatusOK, bills)
}

func (svc *serviceContext) getUserCheckoutsCSV(c *gin.Context) {
	computeID := c.Param("compute_id")
	log.Printf("INFO: get checkouts csv for %s", computeID)
	checkouts, err := svc.getSirsiUserCheckouts(computeID)
	if err != nil {
		log.Printf("ERROR: unable to get user %s checkouts csv: %s", computeID, err.string())
		c.String(err.StatusCode, err.Message)
		return
	}

	var csvRecs [][]string
	colsNames := []string{
		"Id", "Title", "Author", "Barcode", "Call Number", "Library", "Current Location", "Due",
		"Over Due", "Overdue Fee", "Bills", "Recall Due Date", "Renew Date"}
	csvRecs = append(csvRecs, colsNames)
	for _, co := range checkouts {
		var bills []string
		if len(co.Bills) > 0 {
			for _, b := range co.Bills {
				r := fmt.Sprintf("{reason: %s, amount: %s}", b.Label, b.Amount)
				bills = append(bills, r)
			}
		}
		row := []string{
			co.ID, co.Title, co.Author, co.Barcode, co.CallNumber, co.Library, co.CurrentLocation,
			co.Due, fmt.Sprintf("%t", co.OverDue), co.OverdueFee, strings.Join(bills, ","), co.RecallDueDate, co.RenewDate,
		}
		csvRecs = append(csvRecs, row)
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_checkouts.csv", computeID))
	c.Header("Content-Type", "text/csv")
	csvW := csv.NewWriter(c.Writer)
	csvW.WriteAll(csvRecs)
}

// ARK3CX and pc4v have bills on dev
func (svc *serviceContext) getUserCheckouts(c *gin.Context) {
	computeID := c.Param("compute_id")
	log.Printf("INFO: get checkouts for %s", computeID)
	checkouts, err := svc.getSirsiUserCheckouts(computeID)
	if err != nil {
		log.Printf("ERROR: unable to get user %s checkouts: %s", computeID, err.string())
		c.String(err.StatusCode, err.Message)
		return
	}
	c.JSON(http.StatusOK, checkouts)
}

func (svc *serviceContext) getSirsiUserCheckouts(computeID string) ([]checkoutDetails, *requestError) {
	fields := "blockList{amount,block{description},item{key}},"
	fields += "circRecordList{circulationRule{billStructure{maxFee}},dueDate,overdue,estimatedOverdueAmount,recallDueDate,renewalDate,"
	fields += "library{description},item{key,barcode,currentLocation,call{dispCallNumber,bib{key,author,title}}}}"
	url := fmt.Sprintf("/user/patron/alternateID/%s?i&includeFields=%s", computeID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.SlowHTTPClient, url)
	if sirsiErr != nil {
		return nil, sirsiErr
	}

	var coResp sirsiCheckout
	parseErr := json.Unmarshal(sirsiRaw, &coResp)
	if parseErr != nil {
		return nil, &requestError{StatusCode: http.StatusInternalServerError, Message: parseErr.Error()}
	}

	checkouts := make([]checkoutDetails, 0)
	for _, cr := range coResp.Fields.CircRecordList {
		coCall := cr.Fields.Item.Fields.Call
		bills := make([]checkoutBill, 0)
		for _, br := range coResp.Fields.BlockList {
			if br.Fields.Item.Key == cr.Fields.Item.Key {
				bills = append(bills, checkoutBill{
					Amount: br.Fields.Amount.Amount,
					Label:  br.Fields.Block.Fields.Description,
				})
			}
		}

		coItem := checkoutDetails{ID: coCall.Fields.Bib.Key}
		coItem.Title = coCall.Fields.Bib.Fields.Title
		coItem.Author = coCall.Fields.Bib.Fields.Author
		coItem.Barcode = cr.Fields.Item.Fields.Barcode
		coItem.CallNumber = coCall.Fields.DispCallNumber
		coItem.Library = cr.Fields.Library.Fields.Description
		loc := svc.Locations.find(cr.Fields.Item.Fields.CurrentLocation.Key)
		if loc != nil {
			coItem.CurrentLocation = loc.Description
		}
		coItem.Due = cr.Fields.DueDate
		coItem.OverDue = len(bills) > 0
		coItem.OverdueFee = cr.Fields.EstimatedOverdueAmount.Amount
		coItem.Bills = bills
		coItem.RecallDueDate = cr.Fields.RecallDueDate
		coItem.RenewDate = cr.Fields.RenewalDate

		checkouts = append(checkouts, coItem)
	}
	return checkouts, nil
}

func (svc *serviceContext) getUserHolds(c *gin.Context) {
	computeID := c.Param("compute_id")
	log.Printf("INFO: get holds for %s", computeID)
	fields := "holdRecordList{*,bib{title,author},item{barcode,currentLocation,library,transit{transitReason},call{dispCallNumber}}}"
	url := fmt.Sprintf("/user/patron/alternateID/%s?i&includeFields=%s", computeID, fields)
	sirsiRaw, sirsiErr := svc.sirsiGet(svc.SlowHTTPClient, url)
	if sirsiErr != nil {
		log.Printf("ERROR: get sirsi user %s holds failed: %s", computeID, sirsiErr.string())
		c.String(sirsiErr.StatusCode, sirsiErr.Message)
		return
	}

	var holdResp sirsiHolds
	parseErr := json.Unmarshal(sirsiRaw, &holdResp)
	if parseErr != nil {
		log.Printf("ERROR: unable to parse holds response: %s", parseErr.Error())
		c.String(http.StatusInternalServerError, parseErr.Error())
		return
	}

	holds := make([]holdDetails, 0)
	holdRecs := holdResp.Fields.HoldRecordList
	for _, hr := range holdRecs {
		hold := holdDetails{ID: hr.Key, UserID: computeID}
		hold.Status = hr.Fields.Status
		if hold.Status == "BEING_HELD" {
			hold.Status = fmt.Sprintf("AWAITING PICKUP since %s", hr.Fields.BeingHeldDate)
		}
		hold.PickupLocation = hr.Fields.PickupLibrary.Key
		if hold.PickupLocation == "LEO" {
			hold.PickupLocation = "LEO delivery"
		}
		hold.ItemStatus = hr.Fields.Item.Fields.CurrentLocation.Key
		if hold.ItemStatus == "CHECKEDOUT" && hr.Fields.RecallStatus == "RUSH" {
			hold.ItemStatus = "CHECKED OUT, recalled from borrower."
		} else if hold.ItemStatus == "INTRANSIT " {
			if hr.Fields.Item.Fields.Transit.Fields.TransitReason == "HOLD" {
				hold.ItemStatus = "IN TRANSIT for hold"
			}
		}
		hold.Cancellable = hr.Fields.Status == "PLACED" && hr.Fields.RecallStatus != "RUSH"
		hold.PlacedDate = hr.Fields.PlacedDate
		hold.QueueLength = hr.Fields.QueueLength
		hold.QuePosition = hr.Fields.QueuePosition
		hold.TitleKey = hr.Fields.Bib.Key
		hold.Title = hr.Fields.Bib.Fields.Title
		hold.Author = hr.Fields.Bib.Fields.Author
		hold.CallNumber = hr.Fields.Item.Fields.Call.Fields.DispCallNumber
		hold.Barcode = hr.Fields.Item.Fields.Barcode

		holds = append(holds, hold)
	}

	c.JSON(http.StatusOK, holdResponse{Holds: holds})
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
