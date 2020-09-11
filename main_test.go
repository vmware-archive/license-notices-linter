// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: BSD-2-Clause

package main

import (
	"reflect"
	"testing"
)

func TestSortMapDesc(t *testing.T) {
	m := map[string]int{
		"foo": 3,
		"bar": 4,
		"baz": 1,
		"quz": 2,
		"":    10,
	}

	s := sortMapDesc(m)
	if got, want := s, []string{"bar", "foo", "quz", "baz"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %q, want: %q", got, want)
	}
}