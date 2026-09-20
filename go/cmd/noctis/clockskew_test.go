package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sample(serverAhead time.Duration, roundTrip time.Duration) fetchResult {
	sentAt := nowSec()
	return fetchResult{
		date:      time.Unix(sentAt, 0).Add(serverAhead + roundTrip/2).UTC().Format(http.TimeFormat),
		sentAt:    sentAt,
		roundTrip: roundTrip,
	}
}

func TestFetchStampsTheRequestOnThePluginClock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	timeOffset = -90

	result := fetchOauthUsage("token", "1.0")
	if result.err != "" {
		t.Fatalf("fetch failed: %s", result.err)
	}
	if drift := result.sentAt - nowSec(); drift < -5 || drift > 5 {
		t.Fatalf("sentAt must be taken from the plugin clock, not the wall clock: sentAt %d, plugin now %d", result.sentAt, nowSec())
	}
	if result.roundTrip <= 0 || result.roundTrip > fetchTimeout {
		t.Fatalf("roundTrip should be a small positive interval: got %v", result.roundTrip)
	}
	if offset := clockOffsetFrom(result, 0); offset != 90 {
		t.Fatalf("a plugin clock 90s behind the server should read 90 end to end: got %v", offset)
	}
}

func TestClockOffsetFollowsThePluginClock(t *testing.T) {
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	timeOffset = -90
	result := fetchResult{
		date:      time.Now().UTC().Format(http.TimeFormat),
		sentAt:    nowSec(),
		roundTrip: 200 * time.Millisecond,
	}
	if offset := clockOffsetFrom(result, 0); offset != 90 {
		t.Fatalf("a plugin clock 90s behind the server should read 90: got %v", offset)
	}
}

func TestClockOffsetReportsRealSkew(t *testing.T) {
	if offset := clockOffsetFrom(sample(-90*time.Second, 200*time.Millisecond), 0); offset != -90 {
		t.Fatalf("a clock 90s ahead of the server should read -90: got %v", offset)
	}
	if offset := clockOffsetFrom(sample(90*time.Second, 200*time.Millisecond), 0); offset != 90 {
		t.Fatalf("a clock 90s behind the server should read 90: got %v", offset)
	}
}

func TestClockOffsetDistrustsASlowRoundTrip(t *testing.T) {
	slow := sample(90*time.Second, fetchTimeout+time.Second)
	if offset := clockOffsetFrom(slow, 45); offset != 45 {
		t.Fatalf("a round trip past the fetch timeout cannot establish skew, so 45 must stand: got %v", offset)
	}
	quick := sample(90*time.Second, 200*time.Millisecond)
	if offset := clockOffsetFrom(quick, 45); offset != 90 {
		t.Fatalf("the same reading from a quick round trip should be believed: got %v", offset)
	}
}

func TestClockOffsetKeepsWhatItKnewWhenThereIsNothingToMeasure(t *testing.T) {
	if offset := clockOffsetFrom(fetchResult{date: "not a date"}, 45); offset != 45 {
		t.Fatalf("an unparseable Date must not overwrite a known skew: got %v", offset)
	}
	if offset := clockOffsetFrom(fetchResult{date: time.Now().UTC().Format(http.TimeFormat)}, 45); offset != 45 {
		t.Fatalf("a result carrying no request timestamps must not overwrite a known skew: got %v", offset)
	}
}

func TestClockOffsetForgetsSkewThatIsGone(t *testing.T) {
	if offset := clockOffsetFrom(sample(0, 200*time.Millisecond), 90); offset != 0 {
		t.Fatalf("a clock that now agrees should clear the stored skew: got %v", offset)
	}
}
