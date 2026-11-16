package main

import (
	"strings"
)

func g38c2PaperMarketBar(sourceID, openAt, closeAt string) PaperMarketBarObservation {
	return PaperMarketBarObservation{
		Source: "paper_fixture", SourceObservationID: sourceID,
		InputDataSHA256: strings.Repeat("a", 64), Symbol: "005930", Venue: "KRX", Currency: "KRW",
		Interval: "1d", Timezone: "Asia/Seoul", PriceAdjustment: "unspecified",
		Open: "1000", High: "1010", Low: "990", Close: "1005", Volume: "10000",
		OpenAt: openAt, CloseAt: closeAt, SourceAvailableAt: "2026-01-09T06:31:00Z", FetchedAt: "2026-01-09T06:32:00Z",
	}
}
