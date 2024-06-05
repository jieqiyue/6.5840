package raft

import "log"

// Debugging
const Debug = false
const Debugt = false

func DPrintf(format string, a ...interface{}) {
	if Debug {
		log.Printf(format, a...)
	}
}
func DPrintft(format string, a ...interface{}) {
	if Debugt {
		log.Printf("raft:"+format, a...)
	}
}
