package shorten

import "testing"

func TestStatistics(t *testing.T) {
	sut := NewStatsJSON()

	if sut.ServerStats.TotalURL != 0 {
		t.Logf("Incorrect total URL value after init, got: %v, want: %v.", sut.ServerStats.TotalURL, 0)
	}

	if sut.ServerStats.Redirects.Failed != 0 {
		t.Logf("Incorrect failed redirect value after init, got: %v, want: %v.", sut.ServerStats.Redirects.Failed, 0)
	}

	if sut.ServerStats.Redirects.Success != 0 {
		t.Logf("Incorrect succeeded redirect value after init, got: %v, want: %v.", sut.ServerStats.Redirects.Success, 0)
	}

	if sut.ServerStats.Handlers[0].Count != 0 {
		t.Logf("Incorrect handler count value after init, got: %v, want: %v.", sut.ServerStats.Handlers[0].Count, 0)
	}
}

func TestUpdateTotalURL(t *testing.T) {
	tests := []struct {
		totalURL     int64
		wantTotalURL int64
	}{
		{0, 0},
		{1, 1},
		{100000, 100000},
	}

	sut := NewStatsJSON()

	for _, test := range tests {
		sut.updateTotalURL(test.totalURL)
		if sut.ServerStats.TotalURL != test.wantTotalURL {
			t.Errorf("Incorrect total URL value, got: %v, want: %v.", sut.ServerStats.TotalURL, test.wantTotalURL)
		}
	}
}

func TestIncrementHandlerCount(t *testing.T) {
	tests := []struct {
		handlerIndex     HandlerIndex
		success          bool
		wantHandlerCount int64
		wantSuccess      int64
		wantFailed       int64
	}{
		{ShortenHandlerIndex, true, 1, 1, 0},
		{ShortenHandlerIndex, false, 2, 1, 1},
		{StatisticsHandlerIndex, true, 1, 2, 1},
		{ExpanderHandlerIndex, true, 1, 3, 1},
		{ExpanderHandlerIndex, false, 2, 3, 2},
		{ExpanderHandlerIndex, false, 3, 3, 3},
		{ExpanderHandlerIndex, false, 4, 3, 4},
	}

	sut := NewStatsJSON()

	for _, test := range tests {
		sut.incrementHandlerCounter(test.handlerIndex, test.success)

		if sut.ServerStats.Redirects.Success != test.wantSuccess {
			t.Errorf("Incorrect success value, got: %v, want: %v.", sut.ServerStats.Redirects.Success, test.wantSuccess)
		}

		if sut.ServerStats.Redirects.Failed != test.wantFailed {
			t.Errorf("Incorrect failed value, got: %v, want: %v.", sut.ServerStats.Redirects.Failed, test.wantFailed)
		}

		handlers := &sut.ServerStats.Handlers
		for i, handler := range *handlers {
			index := handler.index
			if index != test.handlerIndex {
				continue
			}

			if sut.ServerStats.Handlers[i].Count != test.wantHandlerCount {
				t.Errorf("Incorrect count value, got: %v, want: %v.", sut.ServerStats.Handlers[i].Count, test.wantHandlerCount)
			}
		}
	}
}
