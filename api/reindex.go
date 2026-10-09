/*
Copyright 2024 Blnk Finance Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"net/http"

	"errors"
	"github.com/devaccuracy/ledgerforge/internal/apierror"

	ledgerforge "github.com/devaccuracy/ledgerforge"
	"github.com/gin-gonic/gin"
)

// ReindexRequest represents the request body for starting a reindex operation.
type ReindexRequest struct {
	BatchSize int `json:"batch_size"`
}

// StartReindex triggers a full reindex of all data from the database to Typesense.
// The reindex runs asynchronously to avoid HTTP timeouts.
//
// Parameters:
// - c: The Gin context containing the request and response.
//
// Responses:
// - 202 Accepted: Reindex started successfully, returns initial progress.
// - 409 Conflict: If a reindex is already in progress.
func (a Api) StartReindex(c *gin.Context) {
	var req ReindexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.BatchSize = 0
	}

	if req.BatchSize <= 0 {
		req.BatchSize = 1000
	}

	progress, err := a.ledgerforge.StartReindex(req.BatchSize)
	if errors.Is(err, ledgerforge.ErrReindexInProgress) {
		msg := "A reindex operation is already in progress"
		c.JSON(http.StatusConflict, gin.H{
			"error":        msg,
			"progress":     progress,
			"error_detail": apierror.NewErrorResponse(apierror.ErrSrchReindexInProgress, msg, progress).Error,
		})
		return
	}
	if err != nil {
		respondCode(c, apierror.ErrInternalServer, "Failed to start reindex operation", nil)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "Reindex operation started",
		"progress": progress,
	})
}

// GetReindexProgress returns the current progress of the reindex operation.
//
// Parameters:
// - c: The Gin context containing the request and response.
//
// Responses:
// - 200 OK: Returns current progress.
// - 404 Not Found: If no reindex operation has been started.
func (a Api) GetReindexProgress(c *gin.Context) {
	progress, err := a.ledgerforge.GetReindexProgress()
	if err != nil {
		respondCode(c, apierror.ErrSrchReindexNotStarted, "No reindex operation has been started", nil)
		return
	}

	c.JSON(http.StatusOK, progress)
}
