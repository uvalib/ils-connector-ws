package main

import "github.com/gin-gonic/gin"

// RESPONSE FORMAT
//
//	{
//		"availability_list": {
//			 "libraries": [
//				  {
//						"id": 1,
//						"key": "UVA-LIB",
//						"description": "UVA Library",
//						"on_shelf": false,
//						"circulating": true
//				  }
//			 ],
//			 "locations": [
//				  {
//						"id": 1,
//						"key": "FASLIDEREF",
//						"description": "Slide Collection Reference",
//						"online": false,
//						"shadowed": true,
//						"on_shelf": false,
//						"circulating": true
//				  }
//			 ]
//		}
//	}
func (svc *serviceContext) getAvailabilityList(c *gin.Context) {
	// TODO
}
