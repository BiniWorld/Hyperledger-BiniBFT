/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
)

// MarshalBiniBFTOptions serializes binibft options.
func MarshalBiniBFTOptions(op *BiniBFTOptions) ([]byte, error) {
	if copyMd, ok := proto.Clone(op).(*BiniBFTOptions); ok {
		return proto.Marshal(copyMd)
	} else {
		return nil, errors.New("binibft consenter options type mismatch")
	}
}
