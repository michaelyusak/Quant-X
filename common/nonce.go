package common

import (
	"sync"
	"time"
)

var (
	nonce int64 = time.Now().UnixNano()

	mut sync.Mutex
)

func GetNonce() int64 {
	mut.Lock()
	defer mut.Unlock()

	nonce++
	return nonce
}

func ResetNonce() {
	mut.Lock()
	nonce = time.Now().UnixNano()
	mut.Unlock()
}
