package main

import (
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type serviceContext struct {
	Version string
	Secrets secretsConfig
}

func intializeService(version string, cfg *serviceConfig) (*serviceContext, error) {
	ctx := serviceContext{Version: version,
		Secrets: cfg.Secrets,
	}

	return &ctx, nil
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
