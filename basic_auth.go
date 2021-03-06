// Copyright 2016 Tamás Gulácsi
//
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package grpcer

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
)

type contextKey string

// BasicAuthKey is the context key for the Basic Auth.
const BasicAuthKey = contextKey("authorization-basic")

// WithBasicAuth returns a context prepared with the given username and password.
func WithBasicAuth(ctx context.Context, username, password string) context.Context {
	return context.WithValue(ctx, BasicAuthKey, username+":"+password)
}

var _ = credentials.PerRPCCredentials(basicAuthCreds{})

type basicAuthCreds struct {
	up string
}

// NewBasicAuth returns a PerRPCCredentials with the username and password.
func NewBasicAuth(username, password string) credentials.PerRPCCredentials {
	return basicAuthCreds{up: username + ":" + password}
}

// RequireTransportSecurity returns true - Basic Auth is unsecure in itself.
func (ba basicAuthCreds) RequireTransportSecurity() bool { return true }

// GetRequestMetadata extracts the authorization data from the context.
func (ba basicAuthCreds) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	var up string
	if upI := ctx.Value(BasicAuthKey); upI != nil {
		up = upI.(string)
	}
	if up == "" {
		up = ba.up
	}
	return map[string]string{"authorization": up}, nil
}

// vim: se noet fileencoding=utf-8:
