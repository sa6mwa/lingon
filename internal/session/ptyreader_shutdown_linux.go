//go:build linux

package session

import "context"

func shutdownPTYReader(stopReader context.CancelFunc, closePTY func(), readerDone <-chan struct{}) {
	stopReader()
	<-readerDone
	closePTY()
}
