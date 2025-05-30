// Copyright 2024 The Erigon Authors
// This file is part of Erigon.
//
// Erigon is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Erigon is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with Erigon. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"errors"
	"fmt"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/state"
	"github.com/erigontech/erigon-lib/types/accounts"
)

var PrunedError = errors.New("old data not available due to pruning")

// HistoryReaderV3 Implements StateReader and StateWriter
type HistoryReaderV3 struct {
	txNum     uint64
	trace     bool
	ttx       kv.TemporalTx
	composite []byte
}

func NewHistoryReaderV3() *HistoryReaderV3 {
	return &HistoryReaderV3{composite: make([]byte, 20+32)}
}

func (hr *HistoryReaderV3) String() string {
	return fmt.Sprintf("txNum:%d", hr.txNum)
}
func (hr *HistoryReaderV3) SetTx(tx kv.TemporalTx) { hr.ttx = tx }
func (hr *HistoryReaderV3) SetTxNum(txNum uint64)  { hr.txNum = txNum }
func (hr *HistoryReaderV3) GetTxNum() uint64       { return hr.txNum }
func (hr *HistoryReaderV3) SetTrace(trace bool)    { hr.trace = trace }

// Gets the txNum where Account, Storage and Code history begins.
// If the node is an archive node all history will be available therefore
// the result will be 0.
//
// For non-archive node old history files get deleted, so this number will vary
// but the goal is to know where the historical data begins.
func (hr *HistoryReaderV3) StateHistoryStartFrom() uint64 {
	var earliestTxNum uint64 = 0
	// get the first txnum where  accounts, storage , and code are all available in history files
	// This is max(HistoryStart(Accounts), HistoryStart(Storage), HistoryStart(Code))
	stateDomainNames := []kv.Domain{kv.AccountsDomain, kv.StorageDomain, kv.CodeDomain}
	for _, domainName := range stateDomainNames {
		domainStartingTxNum := hr.ttx.HistoryStartFrom(domainName)
		if domainStartingTxNum > earliestTxNum {
			earliestTxNum = domainStartingTxNum
		}
	}
	return earliestTxNum
}

func (hr *HistoryReaderV3) ReadSet() map[string]*state.KvList { return nil }
func (hr *HistoryReaderV3) ResetReadSet()                     {}
func (hr *HistoryReaderV3) DiscardReadList()                  {}

func (hr *HistoryReaderV3) ReadAccountData(address common.Address) (*accounts.Account, error) {
	enc, ok, err := hr.ttx.GetAsOf(kv.AccountsDomain, address[:], hr.txNum)
	if err != nil || !ok || len(enc) == 0 {
		if hr.trace {
			fmt.Printf("ReadAccountData [%x] => []\n", address)
		}
		return nil, err
	}
	var a accounts.Account
	if err := accounts.DeserialiseV3(&a, enc); err != nil {
		return nil, fmt.Errorf("ReadAccountData(%x): %w", address, err)
	}
	if hr.trace {
		fmt.Printf("ReadAccountData [%x] => [nonce: %d, balance: %d, codeHash: %x]\n", address, a.Nonce, &a.Balance, a.CodeHash)
	}
	return &a, nil
}

// ReadAccountDataForDebug - is like ReadAccountData, but without adding key to `readList`.
// Used to get `prev` account balance
func (hr *HistoryReaderV3) ReadAccountDataForDebug(address common.Address) (*accounts.Account, error) {
	return hr.ReadAccountData(address)
}

func (hr *HistoryReaderV3) ReadAccountStorage(address common.Address, incarnation uint64, key *common.Hash) ([]byte, error) {
	hr.composite = append(append(hr.composite[:0], address[:]...), key.Bytes()...)
	enc, _, err := hr.ttx.GetAsOf(kv.StorageDomain, hr.composite, hr.txNum)
	if hr.trace {
		fmt.Printf("ReadAccountStorage [%x] [%x] => [%x]\n", address, *key, enc)
	}
	return enc, err
}

func (hr *HistoryReaderV3) ReadAccountCode(address common.Address, incarnation uint64) ([]byte, error) {
	//  must pass key2=Nil here: because Erigon4 does concatinate key1+key2 under the hood
	//code, _, err := hr.ttx.GetAsOf(kv.CodeDomain, address.Bytes(), codeHash.Bytes(), hr.txNum)
	code, _, err := hr.ttx.GetAsOf(kv.CodeDomain, address[:], hr.txNum)
	if hr.trace {
		fmt.Printf("ReadAccountCode [%x] => [%x]\n", address, code)
	}
	return code, err
}

func (hr *HistoryReaderV3) ReadAccountCodeSize(address common.Address, incarnation uint64) (int, error) {
	enc, _, err := hr.ttx.GetAsOf(kv.CodeDomain, address[:], hr.txNum)
	return len(enc), err
}

func (hr *HistoryReaderV3) ReadAccountIncarnation(address common.Address) (uint64, error) {
	enc, ok, err := hr.ttx.GetAsOf(kv.AccountsDomain, address.Bytes(), hr.txNum)
	if err != nil || !ok || len(enc) == 0 {
		if hr.trace {
			fmt.Printf("ReadAccountIncarnation [%x] => [0]\n", address)
		}
		return 0, err
	}
	var a accounts.Account
	if err := a.DecodeForStorage(enc); err != nil {
		return 0, fmt.Errorf("ReadAccountIncarnation(%x): %w", address, err)
	}
	if a.Incarnation == 0 {
		if hr.trace {
			fmt.Printf("ReadAccountIncarnation [%x] => [%d]\n", address, 0)
		}
		return 0, nil
	}
	if hr.trace {
		fmt.Printf("ReadAccountIncarnation [%x] => [%d]\n", address, a.Incarnation-1)
	}
	return a.Incarnation - 1, nil
}

type ResettableStateReader interface {
	StateReader
	SetTx(tx kv.TemporalTx)
	SetTxNum(txn uint64)
	GetTxNum() uint64
	DiscardReadList()
	ReadSet() map[string]*state.KvList
	ResetReadSet()
}

/*
func (s *HistoryReaderV3) ForEachStorage(addr common.Address, startLocation common.Hash, cb func(key, seckey common.Hash, value uint256.Int) bool, maxResults int) error {
	acc, err := s.ReadAccountData(addr)
	if err != nil {
		return err
	}

	var k [length.Addr + length.Incarnation + length.Hash]byte
	copy(k[:], addr[:])
	binary.BigEndian.PutUint64(k[length.Addr:], acc.Incarnation)
	copy(k[length.Addr+length.Incarnation:], startLocation[:])

	//toKey := make([]byte, 4)
	//bigCurrent.FillBytes(toKey)
	//
	//bigStep := big.NewInt(0x100000000)
	//bigStep.Div(bigStep, bigCount)
	//bigCurrent.Add(bigCurrent, bigStep)
	//toKey = make([]byte, 4)
	//bigCurrent.FillBytes(toKey)

	st := btree.New(16)
	min := &storageItem{key: startLocation}
	overrideCounter := 0
	if t, ok := s.storage[addr]; ok {
		t.AscendGreaterOrEqual(min, func(i btree.Item) bool {
			item := i.(*storageItem)
			st.ReplaceOrInsert(item)
			if !item.value.IsZero() {
				copy(lastKey[:], item.key[:])
				// Only count non-zero items
				overrideCounter++
			}
			return overrideCounter < maxResults
		})
	}
	numDeletes := st.Len() - overrideCounter

	var lastKey common.Hash
	iterator := s.ac.IterateStorageHistory(startLocation.Bytes(), nil, s.txNum)
	for iterator.HasNext() {
		k, vs, p := iterator.Next()
		if len(vs) == 0 {
			// Skip deleted entries
			continue
		}
		kLoc := k[20:]
		keyHash, err1 := common.HashData(kLoc)
		if err1 != nil {
			return err1
		}
		//fmt.Printf("seckey: %x\n", seckey)
		si := storageItem{}
		copy(si.key[:], kLoc)
		copy(si.seckey[:], keyHash[:])
		if st.Has(&si) {
			continue
		}
		si.value.SetBytes(vs)
		st.ReplaceOrInsert(&si)
		if bytes.Compare(kLoc, lastKey[:]) > 0 {
			// Beyond overrides
			if st.Len() < maxResults+numDeletes {
				continue
			}
			break
		}

	}

	results := 0
	var innerErr error
	st.AscendGreaterOrEqual(min, func(i btree.Item) bool {
		item := i.(*storageItem)
		if !item.value.IsZero() {
			// Skip if value == 0
			cb(item.key, item.seckey, item.value)
			results++
		}
		return results < maxResults
	})
	return innerErr
}
*/

/*
func (s *PlainState) ForEachStorage(addr common.Address, startLocation common.Hash, cb func(key, seckey common.Hash, value uint256.Int) bool, maxResults int) error {
	st := btree.New(16)
	var k [length.Addr + length.Incarnation + length.Hash]byte
	copy(k[:], addr[:])
	accData, err := DomainGetAsOf(s.tx, s.accHistoryC, s.accChangesC, false , addr[:], s.blockNr)
	if err != nil {
		return err
	}
	var acc accounts.Account
	if err := acc.DecodeForStorage(accData); err != nil {
		log.Error("Error decoding account", "err", err)
		return err
	}
	binary.BigEndian.PutUint64(k[length.Addr:], acc.Incarnation)
	copy(k[length.Addr+length.Incarnation:], startLocation[:])
	var lastKey common.Hash
	overrideCounter := 0
	min := &storageItem{key: startLocation}
	if t, ok := s.storage[addr]; ok {
		t.AscendGreaterOrEqual(min, func(i btree.Item) bool {
			item := i.(*storageItem)
			st.ReplaceOrInsert(item)
			if !item.value.IsZero() {
				copy(lastKey[:], item.key[:])
				// Only count non-zero items
				overrideCounter++
			}
			return overrideCounter < maxResults
		})
	}
	numDeletes := st.Len() - overrideCounter
	if err := WalkAsOfStorage(s.tx, addr, acc.Incarnation, startLocation, s.blockNr, func(kAddr, kLoc, vs []byte) (bool, error) {
		if !bytes.Equal(kAddr, addr[:]) {
			return false, nil
		}
		if len(vs) == 0 {
			// Skip deleted entries
			return true, nil
		}
		keyHash, err1 := common.HashData(kLoc)
		if err1 != nil {
			return false, err1
		}
		//fmt.Printf("seckey: %x\n", seckey)
		si := storageItem{}
		copy(si.key[:], kLoc)
		copy(si.seckey[:], keyHash[:])
		if st.Has(&si) {
			return true, nil
		}
		si.value.SetBytes(vs)
		st.ReplaceOrInsert(&si)
		if bytes.Compare(kLoc, lastKey[:]) > 0 {
			// Beyond overrides
			return st.Len() < maxResults+numDeletes, nil
		}
		return st.Len() < maxResults+overrideCounter+numDeletes, nil
	}); err != nil {
		log.Error("ForEachStorage walk error", "err", err)
		return err
	}
	results := 0
	var innerErr error
	st.AscendGreaterOrEqual(min, func(i btree.Item) bool {
		item := i.(*storageItem)
		if !item.value.IsZero() {
			// Skip if value == 0
			cb(item.key, item.seckey, item.value)
			results++
		}
		return results < maxResults
	})
	return innerErr
}
*/
