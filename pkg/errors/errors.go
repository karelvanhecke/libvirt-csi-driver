// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Unavailable(err error) error {
	return status.Error(codes.Unavailable, err.Error())
}

func AlreadyExists(err error) error {
	return status.Error(codes.AlreadyExists, err.Error())
}

func Internal(err error) error {
	return status.Error(codes.Internal, err.Error())
}

func InvalidArgument(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

func NotFound(err error) error {
	return status.Error(codes.NotFound, err.Error())
}

func FailedPrecondition(err error) error {
	return status.Error(codes.FailedPrecondition, err.Error())
}
