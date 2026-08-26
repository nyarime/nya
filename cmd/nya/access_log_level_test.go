package main

import "testing"

func TestAccessLogLevelNoticeVsInfo(t *testing.T) {
	r := httptestNewRequest("/x.nyam", "Mozilla/5.0 Chrome")
	if shouldLogSendAccessAt(sendAccessLogNotice, r, 200) {
		t.Fatal("notice filters browser nyam")
	}
	if !shouldLogSendAccessAt(sendAccessLogInfo, r, 200) {
		t.Fatal("info logs browser nyam")
	}
}
