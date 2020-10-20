/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package api

import (
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Status indicate the result status of request, success or error
type Status string

// ErrorType is not empty if result status is not success
type ErrorType string

// Result is the common format of all response
type Result struct {
	// ErrorType is the type of result if Status is not success
	ErrorType ErrorType `json:"errorType,omitempty"`
	// Err indicate the error detail
	Err string `json:"error,omitempty"`
	// Data is the real data of result, data may be nil even if Status is success
	Data interface{} `json:"data,omitempty"`
	// Status indicate whether the result is success
	Status Status `json:"status"`
}

// InternalErr make a result with ErrorType ErrorInternal
func InternalErr(err error, format string, args ...interface{}) *Result {
	return &Result{
		ErrorType: ErrorInternal,
		Status:    StatusError,
		Err:       errors.Wrapf(err, format, args...).Error(),
	}
}

// BadDataErr make a result with ErrorType ErrorBadData
func BadDataErr(err error, format string, args ...interface{}) *Result {
	return &Result{
		ErrorType: ErrorBadData,
		Status:    StatusError,
		Err:       errors.Wrapf(err, format, args...).Error(),
	}
}

// Data make a result with data or nil, the Status will be set to StatusSuccess
func Data(data interface{}) *Result {
	return &Result{
		Data:   data,
		Status: StatusSuccess,
	}
}

const (
	// StatusSuccess indicate result Status is success, the data of result is available
	StatusSuccess Status = "success"
	// StatusError indicate result is failed, the data may be empty
	StatusError Status = "error"
	// ErrorBadData indicate that result is failed because the wrong request data
	ErrorBadData ErrorType = "bad_data"
	// ErrorInternal indicate that result is failed because the request data may be right but the server is something wrong
	ErrorInternal ErrorType = "internal"
)

// Wrap return a gin handler function with common result processed
func Wrap(log logrus.FieldLogger, f func(ctx *gin.Context) *Result) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		r := f(ctx)
		if r == nil {
			ctx.Status(200)
			return
		}

		code := 200
		if r.ErrorType != "" {
			log.Error(r.Err)
			code = 503
			if r.ErrorType == ErrorBadData {
				code = 400
			}
		}

		ctx.JSON(code, r)
	}
}
