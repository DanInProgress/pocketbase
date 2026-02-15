// Vendored from github.com/dop251/goja_nodejs
//
// Copyright (c) 2016 Dmitry Panov
// SPDX-License-Identifier: MIT
// See https://github.com/dop251/goja_nodejs/blob/master/LICENSE or
// the pocketbase LICENSE.md for full license text
package goutil

import (
	"math"
	"math/big"
	"reflect"

	"github.com/grafana/sobek"
	"github.com/pocketbase/pocketbase/plugins/esmvm/internal/extern/goja_nodejs/errors"
)

func RequiredIntegerArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int) int64 {
	arg := call.Argument(argIndex)
	if sobek.IsNumber(arg) {
		return arg.ToInteger()
	}
	if sobek.IsUndefined(arg) {
		panic(errors.NewTypeError(r, errors.ErrCodeInvalidArgType, "The \"%s\" argument is required.", name))
	}

	panic(errors.NewArgumentNotNumberTypeError(r, name))
}

func RequiredStrictIntegerArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int) int64 {
	arg := call.Argument(argIndex)
	if sobek.IsNumber(arg) {
		val := arg.ToFloat()
		if val != math.Trunc(val) {
			panic(errors.NewRangeError(r, errors.ErrCodeOutOfRange, "The value of %q is out of range. It must be an integer.", name))
		}
		return int64(val)
	}
	if sobek.IsUndefined(arg) {
		panic(errors.NewTypeError(r, errors.ErrCodeInvalidArgType, "The %q argument is required.", name))
	}

	panic(errors.NewArgumentNotNumberTypeError(r, name))
}

func RequiredFloatArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int) float64 {
	arg := call.Argument(argIndex)
	if sobek.IsNumber(arg) {
		return arg.ToFloat()
	}
	if sobek.IsUndefined(arg) {
		panic(errors.NewTypeError(r, errors.ErrCodeInvalidArgType, "The \"%s\" argument is required.", name))
	}

	panic(errors.NewArgumentNotNumberTypeError(r, name))
}

func CoercedIntegerArgument(call sobek.FunctionCall, argIndex int, defaultValue int64, typeMismatchValue int64) int64 {
	arg := call.Argument(argIndex)
	if sobek.IsNumber(arg) {
		return arg.ToInteger()
	}
	if sobek.IsUndefined(arg) {
		return defaultValue
	}

	return typeMismatchValue
}

func OptionalIntegerArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int, defaultValue int64) int64 {
	arg := call.Argument(argIndex)
	if sobek.IsNumber(arg) {
		return arg.ToInteger()
	}
	if sobek.IsUndefined(arg) {
		return defaultValue
	}

	panic(errors.NewArgumentNotNumberTypeError(r, name))
}

func RequiredBigIntArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int) *big.Int {
	arg := call.Argument(argIndex)
	if sobek.IsUndefined(arg) {
		panic(errors.NewTypeError(r, errors.ErrCodeInvalidArgType, "The \"%s\" argument is required.", name))
	}
	if !sobek.IsBigInt(arg) {
		panic(errors.NewArgumentNotBigIntTypeError(r, name))
	}

	n, _ := arg.Export().(*big.Int)
	if n == nil {
		n = new(big.Int)
	}
	return n
}

func RequiredStringArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int) string {
	arg := call.Argument(argIndex)
	if sobek.IsString(arg) {
		return arg.String()
	}
	if sobek.IsUndefined(arg) {
		panic(errors.NewTypeError(r, errors.ErrCodeInvalidArgType, "The \"%s\" argument is required.", name))
	}

	panic(errors.NewArgumentNotStringTypeError(r, name))
}

func RequiredArrayArgument(r *sobek.Runtime, call sobek.FunctionCall, name string, argIndex int) sobek.Value {
	arg := call.Argument(argIndex)
	if arg.ExportType() != reflect.TypeOf(([]any)(nil)) {
		panic(errors.NewNotCorrectTypeError(r, name, "Array"))
	}
	return arg
}
